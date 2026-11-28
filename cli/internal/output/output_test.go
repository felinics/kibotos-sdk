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
