package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableANSIAlignmentAndQuiet(t *testing.T) {
	t.Parallel()

	var out, errBuf bytes.Buffer
	p := &Printer{
		Out:      &out,
		Err:      &errBuf,
		Format:   FormatTable,
		ColorOut: true,
		ColorErr: false,
	}

	err := p.Table(
		[]string{"ID", "STATUS", "ZONE"},
		[][]string{
			{"wks_1", p.FormatStatus("available"), "global-1"},
			{"wks_2", p.FormatStatus("failed"), "eu-1"},
		},
	)
	if err != nil {
		t.Fatalf("Table error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out.String())
	}
	// Stripping ANSI must yield identical column start positions for ZONE.
	colPos := make([]int, 0, 3)
	for _, line := range lines {
		plain := ansiRegexp.ReplaceAllString(line, "")
		idx := strings.Index(plain, "ZONE")
		if idx == -1 {
			idx = strings.Index(plain, "global-1")
		}
		if idx == -1 {
			idx = strings.Index(plain, "eu-1")
		}
		colPos = append(colPos, idx)
	}
	if colPos[0] != colPos[1] || colPos[1] != colPos[2] {
		t.Fatalf("ANSI table columns misaligned: positions=%v\noutput:\n%s", colPos, out.String())
	}
}

func TestStartSpinnerTruncatesToTerminalWidth(t *testing.T) {
	t.Parallel()

	var out, errBuf bytes.Buffer
	p := &Printer{
		Out:       &out,
		Err:       &errBuf,
		Format:    FormatTable,
		TTYErr:    true,
		TermWidth: 60,
	}

	stop := p.StartSpinner("Waiting for workspace wks_09kk2c1pd0s38rsnddk7gx1p65k30chke5wpecv570wpce0 to reach available...")
	stop("Workspace ready", true)

	frames := strings.Split(errBuf.String(), "\r\033[2K")
	if len(frames) < 2 {
		t.Fatalf("expected spinner frames with line clear, got %q", errBuf.String())
	}
	// Check the initial spinner frame (before the final summary line).
	first := frames[1]
	if visibleWidth(first) >= 60 {
		t.Fatalf("spinner frame exceeded terminal width (got %d >= 60): %q", visibleWidth(first), first)
	}
	if !strings.Contains(first, "…") {
		t.Fatalf("expected truncated ellipsis in narrow terminal frame, got %q", first)
	}
}

func TestLinkOSC8AndVisibleWidth(t *testing.T) {
	t.Parallel()

	u := "https://dash.kibo.to/desktop/wks_123"
	plain := &Printer{ColorOut: false}
	if got := plain.Link(u); got != u {
		t.Fatalf("non-color Link should return plain URL, got %q", got)
	}
	colored := &Printer{ColorOut: true}
	got := colored.Link(u)
	if !strings.HasPrefix(got, "\x1b]8;;"+u+"\x1b\\") || !strings.HasSuffix(got, "\x1b]8;;\x1b\\") {
		t.Fatalf("expected OSC 8 hyperlink wrapper, got %q", got)
	}
	if w := visibleWidth(got); w != len(u) {
		t.Fatalf("visibleWidth(Link) = %d, want %d", w, len(u))
	}
}
