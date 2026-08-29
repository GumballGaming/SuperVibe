package app

import (
	"os"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBaseWorktreeID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "plain worktree", id: "wt-1", want: "wt-1"},
		{name: "pane in worktree", id: "wt-1::terminal-abc", want: "wt-1"},
		{name: "extra segments ignored", id: "wt-1::terminal-abc::tail", want: "wt-1"},
		{name: "empty id", id: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := baseWorktreeID(tt.id); got != tt.want {
				t.Fatalf("baseWorktreeID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestStartTerminalPanesAreIndependent(t *testing.T) {
	a := NewApp()
	a.terms["wt-1::pane-1"] = &TerminalSession{id: "wt-1::pane-1", dir: "/repo"}
	a.terms["wt-1::pane-2"] = &TerminalSession{id: "wt-1::pane-2", dir: "/repo"}

	first := a.terms["wt-1::pane-1"]
	second := a.terms["wt-1::pane-2"]
	first.append("one")
	second.append("two")

	if got := first.snapshot(); got != "one" {
		t.Fatalf("pane 1 scrollback = %q, want %q", got, "one")
	}
	if got := second.snapshot(); got != "two" {
		t.Fatalf("pane 2 scrollback = %q, want %q", got, "two")
	}
	if len(a.terms) != 2 {
		t.Fatalf("terminal count = %d, want 2", len(a.terms))
	}
}

func TestIncompleteUTFTail(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want int // length of the tail that must be held back
	}{
		{name: "complete ascii", in: []byte("ls\r\n"), want: 0},
		{name: "complete rune", in: []byte("a\xe2\x94\x82"), want: 0},
		{name: "truncated 3 byte rune", in: []byte("a\xe2\x94"), want: 2},
		{name: "truncated rune alone", in: []byte("\xe2"), want: 1},
		{name: "empty", in: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := incompleteUTFTail(tt.in)
			if len(got) != tt.want {
				t.Fatalf("incompleteUTFTail(%q) held %d bytes (%q), want %d", tt.in, len(got), got, tt.want)
			}
			if tt.want > 0 && (len(got) == 0 || !utf8.RuneStart(got[0]) || utf8.FullRune(got)) {
				t.Fatalf("held tail %q is not an unfinished rune prefix", got)
			}
		})
	}
}

func TestPumpTerminalForwardsRawOutput(t *testing.T) {
	// Interactive TUIs redraw with cursor movement: the pump must pass every
	// byte through untouched or the redraws stack up as duplicated text.
	raw := "\x1b[2J\x1b[1;1HStarting\x1b[3G 3 apps...\r\n\x1b[32mok\x1b[0m"
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()

	term := &TerminalSession{id: "wt::pane"}
	a := &App{terms: map[string]*TerminalSession{}} // no ctx: emitTerminal no-ops
	done := make(chan struct{})
	go func() {
		a.pumpTerminal(term, pr)
		close(done)
	}()
	if _, err := pw.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	<-done

	if got := term.snapshot(); got != raw {
		t.Fatalf("pump buffered %q, want %q", got, raw)
	}
}

// A read can split a multi-byte rune; the held tail must arrive with the next
// chunk instead of being emitted as two invalid halves.
func TestPumpTerminalRejoinsSplitRunes(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()

	term := &TerminalSession{id: "wt::pane"}
	a := &App{terms: map[string]*TerminalSession{}}
	done := make(chan struct{})
	go func() {
		a.pumpTerminal(term, pr)
		close(done)
	}()
	rune3 := []byte("│")
	if _, err := pw.Write(rune3[:2]); err != nil {
		t.Fatal(err)
	}
	// Let the pump observe the partial rune before the rest arrives.
	time.Sleep(20 * time.Millisecond)
	if _, err := pw.Write(rune3[2:]); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	<-done

	if got := term.snapshot(); got != string(rune3) {
		t.Fatalf("split rune buffered as %q, want %q", []byte(got), rune3)
	}
}
