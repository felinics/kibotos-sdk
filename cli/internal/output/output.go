package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// Format identifies the CLI output encoding.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// Printer writes formatted results to stdout and progress/warnings to stderr.
type Printer struct {
	Out    io.Writer
	Err    io.Writer
	Format Format
}

// IsJSON reports whether the printer is configured for JSON output.
func (p *Printer) IsJSON() bool {
	return p.Format == FormatJSON
}

// JSON encodes v as pretty-printed JSON to stdout.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ErrorJSON encodes a structured error object to stderr so stdout stays strictly clean.
func (p *Printer) ErrorJSON(v any) {
	enc := json.NewEncoder(p.Err)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// Table renders headers and rows as an aligned tabwriter table to stdout.
func (p *Printer) Table(headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(p.Out, 0, 2, 2, ' ', 0)
	if len(headers) > 0 {
		if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// KV represents a single key-value row for detail output.
type KV struct {
	Key   string
	Value string
}

// Details renders aligned key-value pairs to stdout.
func (p *Printer) Details(items []KV) error {
	tw := tabwriter.NewWriter(p.Out, 0, 2, 2, ' ', 0)
	for _, item := range items {
		if _, err := fmt.Fprintf(tw, "%s:\t%s\n", item.Key, item.Value); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// Statusf writes a progress/informational line to stderr.
func (p *Printer) Statusf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.Err, format+"\n", args...)
}

// Warnf writes a warning line to stderr.
func (p *Printer) Warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.Err, "Warning: "+format+"\n", args...)
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
