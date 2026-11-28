package output

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// Format identifies the CLI output encoding.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

var (
	ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	osc8Regexp = regexp.MustCompile("\x1b\\]8;;[^\x1b]*\x1b\\\\")
)

// Printer writes formatted results to stdout and progress/warnings to stderr.
type Printer struct {
	Out       io.Writer
	Err       io.Writer
	Format    Format
	Quiet     bool
	ColorOut  bool
	ColorErr  bool
	TTYErr    bool
	TermWidth int
}

// IsJSON reports whether the printer is configured for JSON output.
func (p *Printer) IsJSON() bool {
	return p.Format == FormatJSON
}

// IsQuiet reports whether the printer should emit minimal ID-only stdout output.
func (p *Printer) IsQuiet() bool {
	return p.Quiet && !p.IsJSON()
}

func style(enabled bool, attrs ...color.Attribute) *color.Color {
	c := color.New(attrs...)
	if enabled {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c
}

// Bold renders s in bold for stdout when color is enabled.
func (p *Printer) Bold(s string) string {
	return style(p.ColorOut, color.Bold).Sprint(s)
}

// Dim renders s in faint/dim style for stdout when color is enabled.
func (p *Printer) Dim(s string) string {
	return style(p.ColorOut, color.Faint).Sprint(s)
}

// Link renders a URL as a clickable OSC 8 terminal hyperlink on stdout when color/TTY is enabled.
func (p *Printer) Link(url string) string {
	return formatLink(url, p.ColorOut)
}

// LinkErr renders a URL as a clickable OSC 8 terminal hyperlink on stderr when color/TTY is enabled.
func (p *Printer) LinkErr(url string) string {
	return formatLink(url, p.ColorErr)
}

func formatLink(url string, enabled bool) string {
	if !enabled || url == "" {
		return url
	}
	styled := style(true, color.FgCyan, color.Underline).Sprint(url)
	return "\x1b]8;;" + url + "\x1b\\" + styled + "\x1b]8;;\x1b\\"
}

// FormatStatus returns a color-coded status string for human-readable stdout.
func (p *Printer) FormatStatus(status string) string {
	if !p.ColorOut {
		return status
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "available", "active", "ready":
		return style(true, color.FgGreen).Sprint(status)
	case "creating", "starting", "stopping", "resizing", "deleting", "draining", "attached":
		return style(true, color.FgYellow).Sprint(status)
	case "failed", "lost", "unavailable":
		return style(true, color.FgRed).Sprint(status)
	case "stopped", "revoked", "expired", "deleted", "unknown":
		return style(true, color.Faint).Sprint(status)
	default:
		return status
	}
}

// JSON encodes v as pretty-printed JSON to stdout.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ID writes a single resource ID to stdout (used in --quiet mode).
func (p *Printer) ID(id string) error {
	_, err := fmt.Fprintln(p.Out, id)
	return err
}

// IDs writes one resource ID per line to stdout (used in --quiet mode).
func (p *Printer) IDs(ids []string) error {
	for _, id := range ids {
		if _, err := fmt.Fprintln(p.Out, id); err != nil {
			return err
		}
	}
	return nil
}

// ErrorJSON encodes a structured error object to stderr so stdout stays strictly clean.
func (p *Printer) ErrorJSON(v any) {
	enc := json.NewEncoder(p.Err)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// Table renders headers and rows as an ANSI-aware aligned table to stdout.
func (p *Printer) Table(headers []string, rows [][]string) error {
	numCols := len(headers)
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	if numCols == 0 {
		return nil
	}

	widths := make([]int, numCols)
	for i, h := range headers {
		if w := visibleWidth(h); w > widths[i] {
			widths[i] = w
		}
	}
	for _, r := range rows {
		for i, cell := range r {
			if w := visibleWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	writeRow := func(cells []string, isHeader bool) error {
		var b strings.Builder
		for i := 0; i < len(cells); i++ {
			cell := cells[i]
			rendered := cell
			if isHeader && p.ColorOut {
				rendered = style(true, color.Bold).Sprint(cell)
			}
			b.WriteString(rendered)
			if i < len(cells)-1 {
				pad := widths[i] - visibleWidth(cell) + 2
				if pad < 2 {
					pad = 2
				}
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		_, err := fmt.Fprintln(p.Out, b.String())
		return err
	}

	if len(headers) > 0 {
		if err := writeRow(headers, true); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := writeRow(row, false); err != nil {
			return err
		}
	}
	return nil
}

// KV represents a single key-value row for detail output.
type KV struct {
	Key   string
	Value string
}

// Details renders aligned key-value pairs to stdout.
func (p *Printer) Details(items []KV) error {
	maxKey := 0
	for _, item := range items {
		if w := visibleWidth(item.Key) + 1; w > maxKey {
			maxKey = w
		}
	}
	for _, item := range items {
		label := item.Key + ":"
		pad := maxKey - visibleWidth(label) + 2
		if pad < 2 {
			pad = 2
		}
		renderedLabel := label
		if p.ColorOut {
			renderedLabel = style(true, color.Faint).Sprint(label)
		}
		if _, err := fmt.Fprintf(p.Out, "%s%s%s\n", renderedLabel, strings.Repeat(" ", pad), item.Value); err != nil {
			return err
		}
	}
	return nil
}

// Statusf writes a progress/informational line to stderr (suppressed in --quiet mode).
func (p *Printer) Statusf(format string, args ...any) {
	if p.IsQuiet() {
		return
	}
	_, _ = fmt.Fprintf(p.Err, format+"\n", args...)
}

// Hintf writes a subtle next-step hint to stderr when running interactively in human mode.
func (p *Printer) Hintf(format string, args ...any) {
	if p.IsJSON() || p.IsQuiet() || !p.TTYErr {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if p.ColorErr {
		_, _ = fmt.Fprintf(p.Err, "%s %s\n", style(true, color.FgCyan).Sprint("Hint:"), msg)
		return
	}
	_, _ = fmt.Fprintf(p.Err, "Hint: %s\n", msg)
}

// Warnf writes a warning line to stderr.
func (p *Printer) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.ColorErr {
		_, _ = fmt.Fprintf(p.Err, "%s %s\n", style(true, color.FgYellow, color.Bold).Sprint("Warning:"), msg)
		return
	}
	_, _ = fmt.Fprintf(p.Err, "Warning: %s\n", msg)
}

// StartSpinner starts a single-line progress indicator on stderr for interactive TTY sessions.
// In non-TTY or JSON mode, it writes a single static status line instead of carriage-return frames.
// Call the returned stop function with a final message (optional) and success boolean.
func (p *Printer) StartSpinner(msg string) func(finalMsg string, ok bool) {
	if p.IsQuiet() {
		return func(string, bool) {}
	}
	if !p.TTYErr || p.IsJSON() {
		p.Statusf("%s", msg)
		return func(string, bool) {}
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex

	draw := func(frame string) {
		elapsed := formatElapsed(time.Since(start))
		f := frame
		if p.ColorErr {
			f = style(true, color.FgCyan).Sprint(frame)
		}
		suffix := fmt.Sprintf("(%s)", elapsed)
		displayMsg := msg
		if width := p.errTermWidth(); width > 0 {
			// Reserve 1 col at the right edge so the terminal never auto-wraps onto a second line.
			budget := width - 1 - visibleWidth(frame) - 1 - 1 - visibleWidth(suffix)
			if budget < 4 {
				budget = 4
			}
			displayMsg = truncateRunes(msg, budget)
		}
		_, _ = fmt.Fprintf(p.Err, "\r\033[2K%s %s %s", f, displayMsg, style(p.ColorErr, color.Faint).Sprint(suffix))
	}

	mu.Lock()
	draw(frames[0])
	mu.Unlock()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		idx := 1
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				draw(frames[idx%len(frames)])
				mu.Unlock()
				idx++
			}
		}
	}()

	var once sync.Once
	return func(finalMsg string, ok bool) {
		once.Do(func() {
			close(done)
			wg.Wait()
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprint(p.Err, "\r\033[2K")
			if finalMsg == "" {
				return
			}
			elapsed := formatElapsed(time.Since(start))
			if ok {
				icon := style(p.ColorErr, color.FgGreen).Sprint("✓")
				_, _ = fmt.Fprintf(p.Err, "%s %s %s\n", icon, finalMsg, style(p.ColorErr, color.Faint).Sprintf("(%s)", elapsed))
			} else {
				icon := style(p.ColorErr, color.FgRed).Sprint("✗")
				_, _ = fmt.Fprintf(p.Err, "%s %s %s\n", icon, finalMsg, style(p.ColorErr, color.Faint).Sprintf("(%s)", elapsed))
			}
		})
	}
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

func visibleWidth(s string) int {
	plain := osc8Regexp.ReplaceAllString(s, "")
	plain = ansiRegexp.ReplaceAllString(plain, "")
	return utf8.RuneCountInString(plain)
}

func (p *Printer) errTermWidth() int {
	if p.TermWidth > 0 {
		return p.TermWidth
	}
	if f, ok := p.Err.(interface{ Fd() uintptr }); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// FormatTime formats a timestamp consistently in RFC3339 UTC.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// FormatTimePtr formats a nullable timestamp pointer.
func FormatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// FormatLabels formats a string map as sorted "k=v, k2=v2", or "-" if empty.
func FormatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}

// EmptyDash returns "-" if s is empty, otherwise s.
func EmptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
