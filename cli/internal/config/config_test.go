package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStorePathPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("KIBO_CONFIG_DIR wins", func(t *testing.T) {
		env := map[string]string{
			"KIBO_CONFIG_DIR":    "/tmp/kibo-dir",
			"KIBOTOS_CONFIG_DIR": "/tmp/kibotos-dir",
			"XDG_CONFIG_HOME":    "/tmp/xdg",
		}
		s := NewStore(func(k string) string { return env[k] })
		got, err := s.Path()
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		want := filepath.Join("/tmp/kibo-dir", "config.json")
		if got != want {
			t.Fatalf("Path() = %q, want %q", got, want)
		}
	})

	t.Run("KIBOTOS_CONFIG_DIR over XDG", func(t *testing.T) {
		env := map[string]string{
			"KIBOTOS_CONFIG_DIR": "/tmp/kibotos-dir",
			"XDG_CONFIG_HOME":    "/tmp/xdg",
		}
		s := NewStore(func(k string) string { return env[k] })
		got, err := s.Path()
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		want := filepath.Join("/tmp/kibotos-dir", "config.json")
		if got != want {
			t.Fatalf("Path() = %q, want %q", got, want)
		}
	})

	t.Run("XDG_CONFIG_HOME appends kibo", func(t *testing.T) {
		env := map[string]string{
			"XDG_CONFIG_HOME": "/tmp/xdg",
		}
		s := NewStore(func(k string) string { return env[k] })
		got, err := s.Path()
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		want := filepath.Join("/tmp/xdg", "kibo", "config.json")
		if got != want {
			t.Fatalf("Path() = %q, want %q", got, want)
		}
	})
}

func TestStoreSaveAndLoadPermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	store := NewStoreAt(path, func(string) string { return "" })

	// Loading non-existent file returns empty initialized config.
	empty, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(empty.Profiles) != 0 {
		t.Fatalf("expected empty profiles, got %d", len(empty.Profiles))
	}

	cfg := &Config{
		CurrentProfile: "prod",
		Profiles: map[string]*Profile{
			"prod": {
				APIKey:           "kb_1234567890abcdef",
				APIURL:           "https://api.kibo.to/v1",
				ConsoleURL:       "https://dash.kibo.to",
				OrganizationID:   "org_123",
				OrganizationName: "Acme",
				UserEmail:        "alice@acme.com",
				KeyID:            "key_456",
			},
		},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("config.json permissions = %o, want 0600", perm)
		}
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Save error = %v", err)
	}
	if reloaded.CurrentProfile != "prod" {
		t.Fatalf("CurrentProfile = %q, want prod", reloaded.CurrentProfile)
	}
	got := reloaded.Profiles["prod"]
	if got == nil || got.APIKey != "kb_1234567890abcdef" || got.OrganizationID != "org_123" {
		t.Fatalf("unexpected reloaded profile: %+v", got)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		CurrentProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				APIKey:           "kb_default_key_1111",
				APIURL:           "https://default.kibo.to/v1",
				ConsoleURL:       "https://default-dash.kibo.to",
				OrganizationID:   "org_def",
				OrganizationName: "Default Org",
				UserEmail:        "def@example.com",
				KeyID:            "key_def",
			},
			"staging": {
				APIKey:           "kb_staging_key_2222",
				APIURL:           "https://staging.kibo.to/v1",
				ConsoleURL:       "https://staging-dash.kibo.to",
				OrganizationID:   "org_stg",
				OrganizationName: "Staging Org",
				UserEmail:        "stg@example.com",
				KeyID:            "key_stg",
			},
		},
	}

	t.Run("flag overrides env and profile", func(t *testing.T) {
		env := map[string]string{
			"KIBOTOS_API_KEY": "kb_env_kibotos",
			"KIBO_API_KEY":    "kb_env_kibo",
			"KIBOTOS_API_URL": "https://env.kibo.to/v1",
			"KIBO_PROFILE":    "staging",
		}
		s := NewStore(func(k string) string { return env[k] })
		res := s.Resolve(cfg, Overrides{
			Profile: "default",
			APIKey:  "kb_flag_key",
			APIURL:  "https://flag.kibo.to/v1",
		})
		if res.ProfileName != "default" {
			t.Fatalf("ProfileName = %q, want default", res.ProfileName)
		}
		if res.APIKey != "kb_flag_key" || res.AuthSource != "flag" {
			t.Fatalf("APIKey=%q AuthSource=%q", res.APIKey, res.AuthSource)
		}
		if res.APIURL != "https://flag.kibo.to/v1" {
			t.Fatalf("APIURL = %q, want flag URL", res.APIURL)
		}
		if res.OrganizationID != "" {
			t.Fatalf("expected cleared org metadata when overridden by different key, got %q", res.OrganizationID)
		}
	})

	t.Run("KIBOTOS_API_KEY takes precedence over KIBO_API_KEY and profile", func(t *testing.T) {
		env := map[string]string{
			"KIBOTOS_API_KEY": "kb_env_kibotos",
			"KIBO_API_KEY":    "kb_env_kibo",
			"KIBO_API_URL":    "https://kibo-env.kibo.to/v1",
		}
		s := NewStore(func(k string) string { return env[k] })
		res := s.Resolve(cfg, Overrides{})
		if res.APIKey != "kb_env_kibotos" || res.AuthSource != "env:KIBOTOS_API_KEY" {
			t.Fatalf("APIKey=%q AuthSource=%q", res.APIKey, res.AuthSource)
		}
		if res.APIURL != "https://kibo-env.kibo.to/v1" {
			t.Fatalf("APIURL = %q", res.APIURL)
		}
	})

	t.Run("KIBO_PROFILE selects staging profile", func(t *testing.T) {
		env := map[string]string{
			"KIBO_PROFILE": "staging",
		}
		s := NewStore(func(k string) string { return env[k] })
		res := s.Resolve(cfg, Overrides{})
		if res.ProfileName != "staging" || res.AuthSource != "profile:staging" {
			t.Fatalf("ProfileName=%q AuthSource=%q", res.ProfileName, res.AuthSource)
		}
		if res.APIKey != "kb_staging_key_2222" || res.OrganizationID != "org_stg" {
			t.Fatalf("unexpected resolved staging profile: %+v", res)
		}
	})
}

func TestMaskAPIKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"short", "****"},
		{"kb_1234567890abcdef", "kb_...cdef"},
		{"custom_token_9876", "cus...9876"},
	}
	for _, tc := range cases {
		if got := MaskAPIKey(tc.in); got != tc.want {
			t.Errorf("MaskAPIKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
