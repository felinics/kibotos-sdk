package prompt

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPrompterSelectInputAndConfirm(t *testing.T) {
	t.Parallel()

	opts := []Option{
		{Value: "python-3.12", Label: "python-3.12", Description: "Python 3.12"},
		{Value: "node-22", Label: "node-22", Description: "Node 22"},
	}

	// Non-TTY fallback: empty enters default, substring matches uniquely, invalid retries cleanly.
	in := strings.NewReader("\nnode\n99\n2\n")
	var out bytes.Buffer
	p := New(in, &out, false)

	got, err := p.Select("Pick:", "Template", opts, 0)
	if err != nil || got.Value != "python-3.12" {
		t.Fatalf("default select = (%+v, %v), want python-3.12", got, err)
	}
	got, err = p.Select("Pick:", "Template", opts, 0)
	if err != nil || got.Value != "node-22" {
		t.Fatalf("substring select = (%+v, %v), want node-22", got, err)
	}
	got, err = p.Select("Pick:", "Template", opts, 0)
	if err != nil || got.Value != "node-22" {
		t.Fatalf("retry select = (%+v, %v), want node-22", got, err)
	}

	// Confirm and EOF cancellation.
	p2 := New(strings.NewReader("yes\n\n"), &out, false)
	if ok, err := p2.Confirm("Sure?", false); err != nil || !ok {
		t.Fatalf("Confirm yes = (%v, %v)", ok, err)
	}
	if ok, err := p2.Confirm("Sure?", false); err != nil || ok {
		t.Fatalf("Confirm default false = (%v, %v)", ok, err)
	}
	if _, err := p2.Confirm("Sure?", false); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Confirm EOF err = %v, want ErrCancelled", err)
	}
}

func TestDecodeKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  []byte
		want keyEvent
	}{
		{[]byte{'\x1b', '[', 'A'}, keyUp},
		{[]byte{'\x1b', '[', 'B'}, keyDown},
		{[]byte{'\x1b', 'O', 'A'}, keyUp},
		{[]byte{'\x1b', 'O', 'B'}, keyDown},
		{[]byte{'\x1b', '[', 'H'}, keyHome},
		{[]byte{'\x1b', '[', 'F'}, keyEnd},
		{[]byte{'k'}, keyUp},
		{[]byte{'j'}, keyDown},
		{[]byte{'\r'}, keyEnter},
		{[]byte{'\n'}, keyEnter},
		{[]byte{'\x1b'}, keyCancel},
		{[]byte{0x03}, keyCancel},
		{[]byte{'q'}, keyCancel},
		{[]byte{'\x1b', '[', 'C'}, keyUnknown},
	}
	for _, tc := range cases {
		if got := decodeKey(tc.raw); got != tc.want {
			t.Errorf("decodeKey(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestArrowKeySelectNavigationAndCancel(t *testing.T) {
	t.Parallel()

	opts := []Option{
		{Value: "debian", Label: "debian", Description: "Debian 13"},
		{Value: "ubuntu", Label: "ubuntu", Description: "Ubuntu 24.04"},
		{Value: "alpine", Label: "alpine", Description: "Alpine 3.20"},
	}

	feed := func(events ...keyEvent) func() (keyEvent, error) {
		idx := 0
		return func() (keyEvent, error) {
			if idx >= len(events) {
				return keyCancel, nil
			}
			ev := events[idx]
			idx++
			return ev, nil
		}
	}

	var out bytes.Buffer
	p := New(strings.NewReader(""), &out, false)

	// Down twice -> alpine; Down once more wraps to debian; Up wraps to alpine; Enter confirms.
	got, err := p.selectWithKeyReader("Select a template:", "Template", opts, 0, 80, feed(
		keyDown, keyDown, keyDown, keyUp, keyEnter,
	))
	if err != nil || got.Value != "alpine" {
		t.Fatalf("arrow navigation got (%+v, %v), want alpine", got, err)
	}
	if !strings.Contains(out.String(), "❯") || !strings.Contains(out.String(), "\r\n") {
		t.Fatalf("expected raw-mode arrow UI with ❯ and CRLF, got %q", out.String())
	}

	// Cancel returns ErrCancelled.
	out.Reset()
	if _, err := p.selectWithKeyReader("Select a template:", "Template", opts, 0, 80, feed(keyDown, keyCancel)); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel err = %v, want ErrCancelled", err)
	}

	// Scrolling window with >10 options + Home/End.
	many := make([]Option, 15)
	for i := range many {
		many[i] = Option{Value: fmt.Sprintf("item-%02d", i), Label: fmt.Sprintf("item-%02d", i)}
	}
	out.Reset()
	got, err = p.selectWithKeyReader("Pick item:", "Item", many, 0, 60, feed(keyEnd, keyUp, keyEnter))
	if err != nil || got.Value != "item-13" {
		t.Fatalf("scrolled select got (%+v, %v), want item-13", got, err)
	}
	if !strings.Contains(out.String(), "more") {
		t.Fatalf("expected scroll overflow indicator, got %q", out.String())
	}
}
