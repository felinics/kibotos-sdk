package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

const (
	// DefaultAPIURL is the default Kibotos v1 API base URL.
	DefaultAPIURL = kibotos.DefaultBaseURL
	// DefaultConsoleURL is the default Kibotos web console URL used for CLI login.
	DefaultConsoleURL = "https://console.kibo.to"
	// DefaultProfileName is the default profile name when none is specified.
	DefaultProfileName = "default"
)

// Profile holds credentials and metadata for a single named CLI profile.
type Profile struct {
	APIKey           string `json:"api_key,omitempty"`
	APIURL           string `json:"api_url,omitempty"`
	ConsoleURL       string `json:"console_url,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	UserEmail        string `json:"user_email,omitempty"`
	KeyID            string `json:"key_id,omitempty"`
}

// Config is the persisted JSON structure stored in ~/.config/kibo/config.json.
type Config struct {
	CurrentProfile string              `json:"current_profile,omitempty"`
	Profiles       map[string]*Profile `json:"profiles"`
}

// Store reads and writes the kibo configuration file.
type Store struct {
	path      string
	envLookup func(string) string
}

// NewStore creates a Store using default environment lookup and config path resolution.
func NewStore(envLookup func(string) string) *Store {
	if envLookup == nil {
		envLookup = os.Getenv
	}
	return &Store{envLookup: envLookup}
}

// NewStoreAt creates a Store pinned to a specific config file path.
func NewStoreAt(path string, envLookup func(string) string) *Store {
	if envLookup == nil {
		envLookup = os.Getenv
	}
	return &Store{path: path, envLookup: envLookup}
}

// Path returns the resolved path to config.json.
func (s *Store) Path() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	if dir := strings.TrimSpace(s.envLookup("KIBO_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	if dir := strings.TrimSpace(s.envLookup("KIBOTOS_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	if xdg := strings.TrimSpace(s.envLookup("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "kibo", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "kibo", "config.json"), nil
}

// Load reads the config file, returning an empty initialized Config if it does not exist.
func (s *Store) Load() (*Config, error) {
	path, err := s.Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Profiles: make(map[string]*Profile)}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &Config{Profiles: make(map[string]*Profile)}, nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}
	return &cfg, nil
}

// Save atomically writes cfg to disk with mode 0600 inside a 0700 directory.
func (s *Store) Save(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}
	path, err := s.Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		cleanup()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace config file %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

// Overrides carries per-invocation CLI flag overrides.
type Overrides struct {
	Profile    string
	APIKey     string
	APIURL     string
	ConsoleURL string
}

// Resolved contains the effective configuration for a CLI invocation after applying
// flag > environment > profile > default precedence.
type Resolved struct {
	ProfileName      string
	ProfileExists    bool
	APIKey           string
	AuthSource       string
	APIURL           string
	ConsoleURL       string
	OrganizationID   string
	OrganizationName string
	UserEmail        string
	KeyID            string
}

// Resolve computes the effective credentials and URLs from overrides, environment variables,
// and persisted profile configuration.
func (s *Store) Resolve(cfg *Config, overrides Overrides) Resolved {
	if cfg == nil {
		cfg = &Config{Profiles: make(map[string]*Profile)}
	}

	profileName := firstNonEmpty(
		overrides.Profile,
		s.envLookup("KIBO_PROFILE"),
		s.envLookup("KIBOTOS_PROFILE"),
		cfg.CurrentProfile,
		DefaultProfileName,
	)

	var prof *Profile
	if cfg.Profiles != nil {
		prof = cfg.Profiles[profileName]
	}

	res := Resolved{
		ProfileName:   profileName,
		ProfileExists: prof != nil,
		AuthSource:    "none",
	}

	if prof != nil {
		res.OrganizationID = prof.OrganizationID
		res.OrganizationName = prof.OrganizationName
		res.UserEmail = prof.UserEmail
		res.KeyID = prof.KeyID
	}

	switch {
	case strings.TrimSpace(overrides.APIKey) != "":
		res.APIKey = strings.TrimSpace(overrides.APIKey)
		res.AuthSource = "flag"
	case strings.TrimSpace(s.envLookup("KIBOTOS_API_KEY")) != "":
		res.APIKey = strings.TrimSpace(s.envLookup("KIBOTOS_API_KEY"))
		res.AuthSource = "env:KIBOTOS_API_KEY"
	case strings.TrimSpace(s.envLookup("KIBO_API_KEY")) != "":
		res.APIKey = strings.TrimSpace(s.envLookup("KIBO_API_KEY"))
		res.AuthSource = "env:KIBO_API_KEY"
	case prof != nil && strings.TrimSpace(prof.APIKey) != "":
		res.APIKey = strings.TrimSpace(prof.APIKey)
		res.AuthSource = "profile:" + profileName
	}

	// If auth came from flag or env rather than profile, profile org/email/key metadata
	// may not belong to that overriding key. Clear profile-specific metadata unless
	// the key matches the profile key.
	if res.AuthSource != "profile:"+profileName && (prof == nil || res.APIKey != strings.TrimSpace(prof.APIKey)) {
		res.OrganizationID = ""
		res.OrganizationName = ""
		res.UserEmail = ""
		res.KeyID = ""
	}

	var profAPIURL, profConsoleURL string
	if prof != nil {
		profAPIURL = prof.APIURL
		profConsoleURL = prof.ConsoleURL
	}

	res.APIURL = firstNonEmpty(
		overrides.APIURL,
		s.envLookup("KIBOTOS_API_URL"),
		s.envLookup("KIBO_API_URL"),
		profAPIURL,
		DefaultAPIURL,
	)

	res.ConsoleURL = firstNonEmpty(
		overrides.ConsoleURL,
		s.envLookup("KIBOTOS_CONSOLE_URL"),
		s.envLookup("KIBO_CONSOLE_URL"),
		profConsoleURL,
		DefaultConsoleURL,
	)

	return res
}

// ActiveProfileName returns the configured current profile or DefaultProfileName.
func (c *Config) ActiveProfileName() string {
	if c != nil && strings.TrimSpace(c.CurrentProfile) != "" {
		return strings.TrimSpace(c.CurrentProfile)
	}
	return DefaultProfileName
}

// SortedProfileNames returns all saved profile names in alphabetical order.
func (c *Config) SortedProfileNames() []string {
	if c == nil || len(c.Profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MaskAPIKey returns a safe display representation of an API key, showing only its prefix
// and last 4 characters.
func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	if strings.HasPrefix(key, "kb_") {
		return "kb_..." + key[len(key)-4:]
	}
	return key[:3] + "..." + key[len(key)-4:]
}

// ValidateProfileName checks that a profile name is non-empty and safe.
func ValidateProfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("profile name cannot be empty")
	}
	if strings.ContainsAny(name, " \t\n\r/\\") {
		return fmt.Errorf("invalid profile name %q: must not contain whitespace or path separators", name)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
