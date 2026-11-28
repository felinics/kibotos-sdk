package cli

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

const (
	minMemoryMiB       = 128
	maxMemoryMiB       = 1024 * 1024 // 1 TiB
	minDiskMiB         = 128
	maxDiskMiB         = 16 * 1024 * 1024 // 16 TiB
	minVCPUs           = 1
	maxVCPUs           = 256
	minVolumeGiB       = 1
	maxVolumeGiB       = 65536 // 64 TiB
	minPageLimit       = 1
	maxPageLimit       = 100
	maxExposureTTLSec  = 86400 // 24h (matches regionapi hard cap)
	maxDurationSeconds = 365 * 86400
	maxUserLabels      = 64
	maxResourceNameLen = 128
	maxMountPathLen    = 255
)

var validWorkspaceStatuses = []string{
	string(kibotos.WorkspaceStatusCreating),
	string(kibotos.WorkspaceStatusAvailable),
	string(kibotos.WorkspaceStatusStopping),
	string(kibotos.WorkspaceStatusStopped),
	string(kibotos.WorkspaceStatusStarting),
	string(kibotos.WorkspaceStatusDeleting),
	string(kibotos.WorkspaceStatusFailed),
	string(kibotos.WorkspaceStatusUnknown),
}

var validVolumeStatuses = []string{
	string(kibotos.VolumeStatusCreating),
	string(kibotos.VolumeStatusAvailable),
	string(kibotos.VolumeStatusAttached),
	string(kibotos.VolumeStatusResizing),
	string(kibotos.VolumeStatusDeleting),
	string(kibotos.VolumeStatusFailed),
	string(kibotos.VolumeStatusLost),
	string(kibotos.VolumeStatusUnknown),
}

// parseDurationSeconds parses either a bare positive integer number of seconds (e.g. "3600")
// or a human-friendly duration string such as "30m", "1h", "1h30m", "2d", or "90s".
func parseDurationSeconds(raw, flagName string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, usageError("--%s cannot be empty", flagName)
	}
	// Bare integer -> seconds (100% backwards compatible).
	if sec, err := strconv.Atoi(s); err == nil {
		if sec <= 0 {
			return 0, usageError("--%s must be greater than zero", flagName)
		}
		if sec > maxDurationSeconds {
			return 0, usageError("--%s exceeds maximum allowed duration (%d seconds)", flagName, maxDurationSeconds)
		}
		return sec, nil
	}

	// Support optional leading days like "7d" or "1d12h".
	lower := strings.ToLower(s)
	totalSec := 0.0
	if idx := strings.IndexByte(lower, 'd'); idx != -1 {
		daysPart := lower[:idx]
		days, err := strconv.ParseFloat(daysPart, 64)
		if err != nil || math.IsNaN(days) || math.IsInf(days, 0) || days < 0 {
			return 0, usageError("invalid --%s value %q: expected seconds (e.g. 3600) or duration (e.g. 30m, 1h, 2d)", flagName, raw)
		}
		totalSec += days * 86400
		lower = lower[idx+1:]
	}
	if lower != "" {
		d, err := time.ParseDuration(lower)
		if err != nil || d < 0 {
			return 0, usageError("invalid --%s value %q: expected seconds (e.g. 3600) or duration (e.g. 30m, 1h, 2d)", flagName, raw)
		}
		totalSec += d.Seconds()
	}

	if totalSec < 1 {
		return 0, usageError("--%s must be at least 1 second (got %q)", flagName, raw)
	}
	if totalSec > maxDurationSeconds {
		return 0, usageError("--%s exceeds maximum allowed duration (%d seconds)", flagName, maxDurationSeconds)
	}
	return int(math.Round(totalSec)), nil
}

// parseSizeMiB parses either a bare positive integer in MiB (e.g. "4096")
// or a human-friendly size string with unit suffix (e.g. "512Mi", "4Gi", "4G", "1.5Gi", "20GB").
func parseSizeMiB(raw, flagName string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, usageError("--%s cannot be empty", flagName)
	}
	if mib, err := strconv.Atoi(s); err == nil {
		if mib <= 0 {
			return 0, usageError("--%s must be greater than zero", flagName)
		}
		return mib, nil
	}

	numPart, unitPart := splitNumberAndUnit(s)
	if numPart == "" {
		return 0, usageError("invalid --%s value %q: expected MiB (e.g. 4096) or size with unit (e.g. 4Gi, 20G)", flagName, raw)
	}
	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return 0, usageError("invalid --%s value %q: must be greater than zero", flagName, raw)
	}

	var mult float64
	switch strings.ToLower(unitPart) {
	case "m", "mi", "mb", "mib":
		mult = 1
	case "g", "gi", "gb", "gib":
		mult = 1024
	case "t", "ti", "tb", "tib":
		mult = 1024 * 1024
	default:
		return 0, usageError("invalid --%s unit %q in %q: use Mi/M, Gi/G, or Ti/T (or bare MiB integer)", flagName, unitPart, raw)
	}

	fMiB := math.Round(val * mult)
	if fMiB <= 0 || fMiB > float64(maxDiskMiB) {
		return 0, usageError("--%s value %q is out of range", flagName, raw)
	}
	return int(fMiB), nil
}

// parseWorkspaceMemoryMiB parses and validates workspace memory allocation (>= 128 MiB).
func parseWorkspaceMemoryMiB(raw string) (int, error) {
	mib, err := parseSizeMiB(raw, "memory")
	if err != nil {
		return 0, err
	}
	if mib < minMemoryMiB {
		hint := "Specify a unit such as 1Gi, 4Gi, or at least 128 MiB."
		if isBareInt(raw) {
			hint = fmt.Sprintf("Bare integers are in MiB. Did you mean --memory %sGi?", strings.TrimSpace(raw))
		}
		return 0, usageErrorWithHint(hint, "--memory must be at least %d MiB (got %d MiB)", minMemoryMiB, mib)
	}
	if mib > maxMemoryMiB {
		return 0, usageError("--memory exceeds maximum supported size (%d MiB)", maxMemoryMiB)
	}
	return mib, nil
}

// parseWorkspaceDiskMiB parses and validates workspace root disk allocation (>= 128 MiB).
func parseWorkspaceDiskMiB(raw string) (int, error) {
	mib, err := parseSizeMiB(raw, "disk")
	if err != nil {
		return 0, err
	}
	if mib < minDiskMiB {
		hint := "Specify a unit such as 10Gi, 20Gi, or at least 128 MiB."
		if isBareInt(raw) {
			hint = fmt.Sprintf("Bare integers are in MiB. Did you mean --disk %sGi?", strings.TrimSpace(raw))
		}
		return 0, usageErrorWithHint(hint, "--disk must be at least %d MiB (got %d MiB)", minDiskMiB, mib)
	}
	if mib > maxDiskMiB {
		return 0, usageError("--disk exceeds maximum supported size (%d MiB)", maxDiskMiB)
	}
	return mib, nil
}

// parseSizeGiB parses either a bare positive integer in GiB (e.g. "50")
// or a human-friendly size string with unit suffix (e.g. "50Gi", "50G", "1Ti").
func parseSizeGiB(raw, flagName string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, usageError("--%s cannot be empty", flagName)
	}
	if gib, err := strconv.Atoi(s); err == nil {
		if gib < minVolumeGiB {
			return 0, usageError("--%s must be at least %d GiB", flagName, minVolumeGiB)
		}
		if gib > maxVolumeGiB {
			return 0, usageError("--%s exceeds maximum supported size (%d GiB)", flagName, maxVolumeGiB)
		}
		return gib, nil
	}

	numPart, unitPart := splitNumberAndUnit(s)
	if numPart == "" {
		return 0, usageError("invalid --%s value %q: expected GiB (e.g. 50) or size with unit (e.g. 50Gi, 1Ti)", flagName, raw)
	}
	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return 0, usageError("invalid --%s value %q: must be greater than zero (GiB)", flagName, raw)
	}

	var mult float64
	switch strings.ToLower(unitPart) {
	case "g", "gi", "gb", "gib":
		mult = 1
	case "t", "ti", "tb", "tib":
		mult = 1024
	default:
		return 0, usageError("invalid --%s unit %q in %q: use Gi/G or Ti/T (or bare GiB integer)", flagName, unitPart, raw)
	}

	fGiB := math.Round(val * mult)
	if fGiB < minVolumeGiB {
		return 0, usageError("--%s must be at least %d GiB", flagName, minVolumeGiB)
	}
	if fGiB > maxVolumeGiB {
		return 0, usageError("--%s exceeds maximum supported size (%d GiB)", flagName, maxVolumeGiB)
	}
	return int(fGiB), nil
}

func validateVCPUs(vcpus int) error {
	if vcpus < minVCPUs || vcpus > maxVCPUs {
		return usageError("--vcpus must be between %d and %d (got %d)", minVCPUs, maxVCPUs, vcpus)
	}
	return nil
}

func validatePageLimit(cmd *cobra.Command, limit int) error {
	if !cmd.Flags().Changed("limit") {
		return nil
	}
	if limit < minPageLimit || limit > maxPageLimit {
		return usageError("--limit must be between %d and %d (got %d)", minPageLimit, maxPageLimit, limit)
	}
	return nil
}

func parseExposureTTLSeconds(raw string) (int, error) {
	sec, err := parseDurationSeconds(raw, "ttl")
	if err != nil {
		return 0, err
	}
	if sec > maxExposureTTLSec {
		return 0, usageError("--ttl must be at most 24h (%d seconds, got %d seconds)", maxExposureTTLSec, sec)
	}
	return sec, nil
}

func validateWorkspaceStatus(raw string) (kibotos.WorkspaceStatus, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	for _, allowed := range validWorkspaceStatuses {
		if s == allowed {
			return kibotos.WorkspaceStatus(s), nil
		}
	}
	return "", usageError("invalid workspace status %q: must be one of %s", s, strings.Join(validWorkspaceStatuses, ", "))
}

func validateVolumeStatus(raw string) (kibotos.VolumeStatus, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	for _, allowed := range validVolumeStatuses {
		if s == allowed {
			return kibotos.VolumeStatus(s), nil
		}
	}
	return "", usageError("invalid volume status %q: must be one of %s", s, strings.Join(validVolumeStatuses, ", "))
}

func validateMountPath(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", usageError("--mount-path cannot be empty")
	}
	if len(s) > maxMountPathLen {
		return "", usageError("--mount-path exceeds maximum length (%d characters)", maxMountPathLen)
	}
	if !strings.HasPrefix(s, "/") {
		return "", usageError("invalid --mount-path %q: must be an absolute path starting with '/' (e.g. /data)", raw)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", usageError("invalid --mount-path %q: must not contain whitespace or control characters", raw)
		}
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", usageError("invalid --mount-path %q: path must not contain '..'", raw)
		}
	}
	cleaned := path.Clean(s)
	if cleaned == "/" {
		return "", usageError("invalid --mount-path %q: cannot mount directly at '/' (use a subpath like /data)", raw)
	}
	return cleaned, nil
}

func validateResourceName(raw string, allowEmpty bool) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		if allowEmpty {
			return "", nil
		}
		return "", usageError("--name cannot be empty (use --clear-name to remove an existing name)")
	}
	if len(s) > maxResourceNameLen {
		return "", usageError("name exceeds maximum length of %d characters", maxResourceNameLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", usageError("name must not contain control characters")
		}
	}
	return s, nil
}

func validateNonEmptyFlag(cmd *cobra.Command, flagName, val string) error {
	if cmd.Flags().Changed(flagName) && strings.TrimSpace(val) == "" {
		return usageError("--%s cannot be empty", flagName)
	}
	return nil
}

func isLowerAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func isASCIIAlnum(b byte) bool {
	return isLowerAlnum(b) || (b >= 'A' && b <= 'Z')
}

func validateLabelKey(key string) error {
	if len(key) == 0 || len(key) > 63 || !isLowerAlnum(key[0]) || !isLowerAlnum(key[len(key)-1]) {
		return usageError("invalid label key %q: must be 1-63 chars, start and end with [a-z0-9], and use only lowercase letters, digits, '-', '_', '.'", key)
	}
	for i := 1; i < len(key)-1; i++ {
		b := key[i]
		if !isLowerAlnum(b) && b != '-' && b != '_' && b != '.' {
			return usageError("invalid label key %q: must be 1-63 chars, start and end with [a-z0-9], and use only lowercase letters, digits, '-', '_', '.'", key)
		}
	}
	return nil
}

func validateLabelValue(key, val string) error {
	if len(val) == 0 {
		return nil
	}
	if len(val) > 63 || !isASCIIAlnum(val[0]) || !isASCIIAlnum(val[len(val)-1]) {
		return usageError("invalid label value %q for key %q: must be empty or 1-63 chars, start and end with [A-Za-z0-9], and use only letters, digits, '-', '_', '.'", val, key)
	}
	for i := 1; i < len(val)-1; i++ {
		b := val[i]
		if !isASCIIAlnum(b) && b != '-' && b != '_' && b != '.' {
			return usageError("invalid label value %q for key %q: must be empty or 1-63 chars, start and end with [A-Za-z0-9], and use only letters, digits, '-', '_', '.'", val, key)
		}
	}
	return nil
}

func isBareInt(raw string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(raw))
	return err == nil
}

func splitNumberAndUnit(s string) (string, string) {
	i := 0
	for i < len(s) {
		ch := s[i]
		if (ch >= '0' && ch <= '9') || ch == '.' || (i == 0 && (ch == '+' || ch == '-')) {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
}

// parseTemplateSpec splits an optional "template@revision" specifier and reconciles it with --revision.
func parseTemplateSpec(rawTemplate, rawRevision string) (string, string, error) {
	tpl := strings.TrimSpace(rawTemplate)
	rev := strings.TrimSpace(rawRevision)
	if idx := strings.IndexByte(tpl, '@'); idx != -1 {
		inlineRev := strings.TrimSpace(tpl[idx+1:])
		tpl = strings.TrimSpace(tpl[:idx])
		if tpl == "" {
			return "", "", usageError("invalid --template %q: template ID before '@' cannot be empty", rawTemplate)
		}
		if inlineRev == "" {
			return "", "", usageError("invalid --template %q: revision after '@' cannot be empty", rawTemplate)
		}
		if rev != "" && rev != inlineRev {
			return "", "", usageError("conflicting template revisions: %q in --template vs %q in --revision", inlineRev, rev)
		}
		rev = inlineRev
	}
	return tpl, rev, nil
}

// resolveWait determines whether an async command should wait for completion.
// Waiting is enabled by default; pass --no-wait or --wait=false to return immediately.
func resolveWait(cmd *cobra.Command, waitFlag, noWaitFlag bool, waitTimeoutRaw string) (bool, time.Duration, error) {
	waitChanged := cmd.Flags().Changed("wait")
	noWaitChanged := cmd.Flags().Changed("no-wait")

	if waitChanged && noWaitChanged && waitFlag && noWaitFlag {
		return false, 0, usageError("--wait and --no-wait cannot be used together")
	}

	shouldWait := waitFlag
	if noWaitFlag {
		shouldWait = false
	}

	if !shouldWait {
		return false, 0, nil
	}

	sec, err := parseDurationSeconds(waitTimeoutRaw, "wait-timeout")
	if err != nil {
		return false, 0, err
	}
	return true, time.Duration(sec) * time.Second, nil
}

// addWaitFlags registers the standard --wait (default true), --no-wait, and --wait-timeout flags.
func addWaitFlags(cmd *cobra.Command, wait *bool, noWait *bool, waitTimeout *string, targetDesc string) {
	f := cmd.Flags()
	f.BoolVar(wait, "wait", true, fmt.Sprintf("Wait until %s (default true; pass --no-wait to return immediately)", targetDesc))
	f.BoolVar(noWait, "no-wait", false, "Return immediately without waiting for completion")
	f.StringVar(waitTimeout, "wait-timeout", fmt.Sprintf("%ds", defaultWaitTimeoutSeconds), "Maximum duration to wait (e.g. 5m, 300)")
}
