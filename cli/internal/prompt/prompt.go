package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// ErrCancelled indicates the user aborted an interactive prompt (e.g. EOF / Esc / Ctrl-C).
var ErrCancelled = errors.New("operation cancelled")

// Option represents a single selectable item in an interactive list prompt.
type Option struct {
	Value       string
	Label       string
	Description string
}

// Prompter provides lightweight inline interactive prompts on stderr:
// arrow-key selection on real TTYs, with line-based fallback for non-TTY/tests.
type Prompter struct {
	reader *bufio.Reader
	rawIn  *os.File
	rawOut *os.File
	out    io.Writer
	color  bool
}

// New creates a Prompter that reads from in and prints prompts to out (typically stderr).
func New(in io.Reader, out io.Writer, enableColor bool) *Prompter {
	p := &Prompter{
		reader: bufio.NewReader(in),
		out:    out,
		color:  enableColor,
	}
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		p.rawIn = f
	}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		p.rawOut = f
	}
	return p
}

// RemainingInput returns stdin including bytes buffered by line-based prompts.
func (p *Prompter) RemainingInput() io.Reader {
	return p.reader
}

func (p *Prompter) style(attrs ...color.Attribute) *color.Color {
	c := color.New(attrs...)
	if p.color {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c
}

func (p *Prompter) readLine() (string, error) {
	line, err := p.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if strings.TrimSpace(line) != "" {
				return strings.TrimSpace(line), nil
			}
			return "", ErrCancelled
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Input asks for a line of text with an optional default value and validator.
func (p *Prompter) Input(label, defaultVal string, validate func(string) error) (string, error) {
	for {
		if defaultVal != "" {
			_, _ = fmt.Fprintf(p.out, "%s %s: ",
				p.style(color.Bold).Sprint(label),
				p.style(color.Faint).Sprintf("[%s]", defaultVal),
			)
		} else {
			_, _ = fmt.Fprintf(p.out, "%s: ", p.style(color.Bold).Sprint(label))
		}

		raw, err := p.readLine()
		if err != nil {
			return "", err
		}
		if raw == "" {
			raw = defaultVal
		}
		if validate != nil {
			if vErr := validate(raw); vErr != nil {
				_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.FgRed).Sprintf("  %v", vErr))
				continue
			}
		}
		return raw, nil
	}
}

// Confirm asks a yes/no question with a default answer.
func (p *Prompter) Confirm(label string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	for {
		_, _ = fmt.Fprintf(p.out, "%s %s: ",
			p.style(color.Bold).Sprint(label),
			p.style(color.Faint).Sprint(hint),
		)
		raw, err := p.readLine()
		if err != nil {
			return false, err
		}
		if raw == "" {
			return defaultYes, nil
		}
		switch strings.ToLower(raw) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.FgRed).Sprint("  Please answer y or n."))
		}
	}
}

// Select lets the user pick an option using arrow keys + Enter on a TTY,
// falling back to a numbered line prompt when stdin/stderr are not terminals.
func (p *Prompter) Select(title, fieldLabel string, options []Option, defaultIdx int) (Option, error) {
	if len(options) == 0 {
		return Option{}, errors.New("no options available to select")
	}
	if defaultIdx < 0 || defaultIdx >= len(options) {
		defaultIdx = 0
	}
	if p.rawIn != nil && p.rawOut != nil && p.reader.Buffered() == 0 {
		return p.selectTTY(title, fieldLabel, options, defaultIdx)
	}
	return p.selectLine(title, fieldLabel, options, defaultIdx)
}

type keyEvent int

const (
	keyUnknown keyEvent = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
	keyHome
	keyEnd
)

func decodeKey(buf []byte) keyEvent {
	if len(buf) == 0 {
		return keyUnknown
	}
	switch buf[0] {
	case '\r', '\n':
		return keyEnter
	case 0x03, 0x04, 'q', 'Q': // Ctrl+C, Ctrl+D, q
		return keyCancel
	case 'k', 'K', 0x10: // k or Ctrl+P
		return keyUp
	case 'j', 'J', 0x0e: // j or Ctrl+N
		return keyDown
	case 0x1b: // ESC or escape sequence
		if len(buf) == 1 {
			return keyCancel
		}
		if len(buf) >= 3 && (buf[1] == '[' || buf[1] == 'O') {
			switch buf[2] {
			case 'A':
				return keyUp
			case 'B':
				return keyDown
			case 'H':
				return keyHome
			case 'F':
				return keyEnd
			}
		}
		return keyUnknown
	default:
		return keyUnknown
	}
}

func (p *Prompter) termWidth() int {
	if p.rawOut != nil {
		if w, _, err := term.GetSize(int(p.rawOut.Fd())); err == nil && w > 20 {
			return w
		}
	}
	return 80
}

func (p *Prompter) selectTTY(title, fieldLabel string, options []Option, defaultIdx int) (Option, error) {
	inFd := int(p.rawIn.Fd())
	oldState, err := term.MakeRaw(inFd)
	if err != nil {
		return p.selectLine(title, fieldLabel, options, defaultIdx)
	}
	defer func() {
		_ = term.Restore(inFd, oldState)
	}()

	// Hide cursor while navigating; always restore before returning.
	_, _ = io.WriteString(p.out, "\x1b[?25l")
	defer func() {
		_, _ = io.WriteString(p.out, "\x1b[?25h")
	}()

	buf := make([]byte, 16)
	readKey := func() (keyEvent, error) {
		n, rErr := p.rawIn.Read(buf)
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				return keyCancel, nil
			}
			return keyUnknown, rErr
		}
		return decodeKey(buf[:n]), nil
	}

	chosen, selErr := p.selectWithKeyReader(title, fieldLabel, options, defaultIdx, p.termWidth(), readKey)

	// Restore cooked terminal before printing final collapsed line so \n behaves normally.
	_ = term.Restore(inFd, oldState)
	_, _ = io.WriteString(p.out, "\x1b[?25h")
	if selErr != nil {
		return Option{}, selErr
	}

	lbl := chosen.Label
	if lbl == "" {
		lbl = chosen.Value
	}
	summaryLabel := strings.TrimSpace(fieldLabel)
	if summaryLabel == "" {
		summaryLabel = strings.TrimSuffix(strings.TrimSpace(title), ":")
	}
	_, _ = fmt.Fprintf(p.out, "%s %s %s\n",
		p.style(color.FgGreen, color.Bold).Sprint("✓"),
		p.style(color.Bold).Sprint(summaryLabel+":"),
		p.style(color.FgCyan).Sprint(lbl),
	)
	return chosen, nil
}

const maxVisibleSelectRows = 10

func (p *Prompter) selectWithKeyReader(
	title, fieldLabel string,
	options []Option,
	defaultIdx int,
	width int,
	readKey func() (keyEvent, error),
) (Option, error) {
	if width <= 20 {
		width = 80
	}

	cursor := defaultIdx
	offset := 0
	maxRows := maxVisibleSelectRows
	if len(options) < maxRows {
		maxRows = len(options)
	}

	adjustOffset := func() {
		if cursor < offset {
			offset = cursor
		}
		if cursor >= offset+maxRows {
			offset = cursor - maxRows + 1
		}
		if offset < 0 {
			offset = 0
		}
		if maxOff := len(options) - maxRows; offset > maxOff {
			offset = maxOff
		}
	}
	adjustOffset()

	maxLabel := 0
	for _, opt := range options {
		lbl := optionLabel(opt)
		if w := displayWidth(lbl); w > maxLabel {
			maxLabel = w
		}
	}

	cleanTitle := strings.TrimSuffix(strings.TrimSpace(title), ":")
	hintText := "(↑/↓ move, Enter confirm)"
	if len(options) == 1 {
		hintText = "(Enter confirm)"
	}

	renderedLines := 0
	clearRendered := func() {
		if renderedLines <= 0 {
			return
		}
		_, _ = fmt.Fprintf(p.out, "\r\x1b[%dA\x1b[J", renderedLines)
		renderedLines = 0
	}

	render := func() {
		clearRendered()

		var b strings.Builder
		lines := 0

		// Header line
		headerPlainLen := 2 + displayWidth(cleanTitle) + 2 + displayWidth(hintText)
		if headerPlainLen < width {
			fmt.Fprintf(&b, "%s %s  %s\r\n",
				p.style(color.FgCyan, color.Bold).Sprint("?"),
				p.style(color.Bold).Sprint(cleanTitle),
				p.style(color.Faint).Sprint(hintText),
			)
		} else {
			fmt.Fprintf(&b, "%s %s\r\n",
				p.style(color.FgCyan, color.Bold).Sprint("?"),
				p.style(color.Bold).Sprint(truncateDisplay(cleanTitle, width-3)),
			)
		}
		lines++

		if offset > 0 {
			fmt.Fprintf(&b, "  %s\r\n", p.style(color.Faint).Sprintf("↑ %d more", offset))
			lines++
		}

		end := offset + maxRows
		if end > len(options) {
			end = len(options)
		}
		for i := offset; i < end; i++ {
			opt := options[i]
			lbl := optionLabel(opt)
			selected := i == cursor

			prefix := "  "
			if selected {
				prefix = p.style(color.FgCyan, color.Bold).Sprint("❯") + " "
			}

			// Compute safe description truncation so the line never wraps in the terminal.
			availAfterPrefix := width - 3 // 2-col prefix + 1-col safety margin
			lblDisplay := truncateDisplay(lbl, availAfterPrefix)
			lblWidth := displayWidth(lblDisplay)

			var styledLbl string
			if selected {
				styledLbl = p.style(color.FgCyan, color.Bold).Sprint(lblDisplay)
			} else {
				styledLbl = lblDisplay
			}

			if opt.Description != "" {
				padCount := maxLabel - lblWidth + 2
				if padCount < 2 {
					padCount = 2
				}
				rem := availAfterPrefix - lblWidth - padCount
				if rem >= 6 {
					desc := truncateDisplay(opt.Description, rem)
					fmt.Fprintf(&b, "%s%s%s%s\r\n",
						prefix,
						styledLbl,
						strings.Repeat(" ", padCount),
						p.style(color.Faint).Sprint(desc),
					)
				} else {
					fmt.Fprintf(&b, "%s%s\r\n", prefix, styledLbl)
				}
			} else {
				fmt.Fprintf(&b, "%s%s\r\n", prefix, styledLbl)
			}
			lines++
		}

		if remaining := len(options) - end; remaining > 0 {
			fmt.Fprintf(&b, "  %s\r\n", p.style(color.Faint).Sprintf("↓ %d more", remaining))
			lines++
		}

		_, _ = io.WriteString(p.out, b.String())
		renderedLines = lines
	}

	render()
	for {
		ev, err := readKey()
		if err != nil {
			clearRendered()
			return Option{}, err
		}
		switch ev {
		case keyUp:
			cursor = (cursor - 1 + len(options)) % len(options)
			adjustOffset()
			render()
		case keyDown:
			cursor = (cursor + 1) % len(options)
			adjustOffset()
			render()
		case keyHome:
			cursor = 0
			adjustOffset()
			render()
		case keyEnd:
			cursor = len(options) - 1
			adjustOffset()
			render()
		case keyEnter:
			clearRendered()
			return options[cursor], nil
		case keyCancel:
			clearRendered()
			return Option{}, ErrCancelled
		}
	}
}

func optionLabel(opt Option) string {
	if opt.Label != "" {
		return opt.Label
	}
	return opt.Value
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && ((r <= 0x115f) || r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe19) ||
			(r >= 0xfe30 && r <= 0xfe6f) ||
			(r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x20000 && r <= 0x3fffd)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func truncateDisplay(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	if displayWidth(s) <= maxCols {
		return s
	}
	if maxCols == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := displayWidth(string(r))
		if w+rw > maxCols-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteRune('…')
	return b.String()
}

// selectLine is the non-TTY fallback (used in tests and piped sessions).
func (p *Prompter) selectLine(title, fieldLabel string, options []Option, defaultIdx int) (Option, error) {
	_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.Bold).Sprint(title))

	maxLabel := 0
	for _, opt := range options {
		lbl := optionLabel(opt)
		if len(lbl) > maxLabel {
			maxLabel = len(lbl)
		}
	}

	numWidth := len(strconv.Itoa(len(options)))
	for i, opt := range options {
		lbl := optionLabel(opt)
		num := fmt.Sprintf("%*d)", numWidth, i+1)
		if opt.Description != "" {
			pad := maxLabel - len(lbl) + 2
			_, _ = fmt.Fprintf(p.out, "  %s %s%s%s\n",
				p.style(color.FgCyan).Sprint(num),
				lbl,
				strings.Repeat(" ", pad),
				p.style(color.Faint).Sprint(opt.Description),
			)
		} else {
			_, _ = fmt.Fprintf(p.out, "  %s %s\n",
				p.style(color.FgCyan).Sprint(num),
				lbl,
			)
		}
	}

	if fieldLabel == "" {
		fieldLabel = "Select"
	}

	for {
		_, _ = fmt.Fprintf(p.out, "%s %s: ",
			p.style(color.Bold).Sprint(fieldLabel),
			p.style(color.Faint).Sprintf("[%d]", defaultIdx+1),
		)

		raw, err := p.readLine()
		if err != nil {
			return Option{}, err
		}
		if raw == "" {
			return options[defaultIdx], nil
		}

		// Match 1-based number
		if idx, convErr := strconv.Atoi(raw); convErr == nil {
			if idx >= 1 && idx <= len(options) {
				return options[idx-1], nil
			}
			_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.FgRed).Sprintf("  Enter a number between 1 and %d.", len(options)))
			continue
		}

		// Exact match on value or label (case-insensitive)
		lower := strings.ToLower(raw)
		for _, opt := range options {
			if strings.EqualFold(opt.Value, raw) || (opt.Label != "" && strings.EqualFold(opt.Label, raw)) {
				return opt, nil
			}
		}

		// Unique substring match on value or label
		var matches []Option
		for _, opt := range options {
			if strings.Contains(strings.ToLower(opt.Value), lower) ||
				(opt.Label != "" && strings.Contains(strings.ToLower(opt.Label), lower)) {
				matches = append(matches, opt)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.FgRed).Sprintf("  %q matches %d options; enter a number (1-%d) or more specific text.", raw, len(matches), len(options)))
			continue
		}
		_, _ = fmt.Fprintf(p.out, "%s\n", p.style(color.FgRed).Sprintf("  Invalid selection %q; enter 1-%d or a matching name.", raw, len(options)))
	}
}
