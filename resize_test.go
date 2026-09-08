package bubblessh

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func lineText(t *testing.T, term *vt.Emulator, y, cols int) string {
	t.Helper()
	s := make([]byte, 0, cols)
	for x := 0; x < cols; x++ {
		c := term.CellAt(x, y)
		if c == nil || c.Content == "" {
			s = append(s, ' ')
			continue
		}
		s = append(s, c.Content...)
	}
	return string(s)
}

func newSizedModel(t *testing.T, cols, rows int, content string) Model {
	t.Helper()
	term := vt.NewEmulator(cols, rows)
	if content != "" {
		term.WriteString(content)
	}
	return Model{vt: term, width: cols, height: rows}
}

func TestReflowRestoresWidthOnGrow(t *testing.T) {
	m := newSizedModel(t, 10, 4, "0123456789")

	m, _ = m.SetSize(5, 4)
	if got := lineText(t, m.vt, 0, 5); got != "01234" {
		t.Fatalf("after shrink, row 0 = %q, want %q", got, "01234")
	}
	if got := lineText(t, m.vt, 1, 5); got != "56789" {
		t.Fatalf("after shrink, row 1 = %q, want the overflow wrapped onto the next row %q", got, "56789")
	}

	m, _ = m.SetSize(10, 4)
	got := lineText(t, m.vt, 0, 10)
	if got != "0123456789" {
		t.Errorf("after grow, row 0 = %q, want the original unwrapped row %q", got, "0123456789")
	}
}

func TestReflowRestoresAfterStepwiseShrink(t *testing.T) {
	m := newSizedModel(t, 10, 4, "0123456789")

	m, _ = m.SetSize(8, 4)
	m, _ = m.SetSize(6, 4)
	m, _ = m.SetSize(10, 4)

	got := lineText(t, m.vt, 0, 10)
	if got != "0123456789" {
		t.Errorf("after stepwise shrink/grow, row 0 = %q, want %q", got, "0123456789")
	}
}

func TestReflowRestoresHeightOnGrow(t *testing.T) {
	m := newSizedModel(t, 5, 3, "row0\r\nrow1\r\nrow2")

	m, _ = m.SetSize(5, 1)
	m, _ = m.SetSize(5, 3)

	if got := lineText(t, m.vt, 0, 5); got != "row0 " {
		t.Errorf("row 0 = %q, want %q", got, "row0 ")
	}
	if got := lineText(t, m.vt, 1, 5); got != "row1 " {
		t.Errorf("row 1 = %q, want %q", got, "row1 ")
	}
	if got := lineText(t, m.vt, 2, 5); got != "row2 " {
		t.Errorf("row 2 = %q, want %q", got, "row2 ")
	}
}

func TestReflowSkipsRowsThatAlreadyFit(t *testing.T) {
	m := newSizedModel(t, 10, 2, "hi")

	m, _ = m.SetSize(5, 2)
	if got := lineText(t, m.vt, 0, 5); got != "hi   " {
		t.Errorf("short row after shrink = %q, want %q", got, "hi   ")
	}

	m, _ = m.SetSize(10, 2)
	if got := lineText(t, m.vt, 0, 10); got != "hi        " {
		t.Errorf("short row after grow = %q, want %q", got, "hi        ")
	}
}

func TestStripLeadingCRLF(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"\nbandit0@bandit:~$ ", "bandit0@bandit:~$ "},
		{"\r\nbandit0@bandit:~$ ", "bandit0@bandit:~$ "},
		{"\rbandit0@bandit:~$ ", "bandit0@bandit:~$ "},
		{"bandit0@bandit:~$ ", "bandit0@bandit:~$ "},
		{"", ""},
	}
	for _, c := range cases {
		if got := string(stripLeadingCRLF([]byte(c.in))); got != c.want {
			t.Errorf("stripLeadingCRLF(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEatWinchNewlineDoesNotInsertPromptLine(t *testing.T) {
	m := newSizedModel(t, 20, 4, "bandit0@bandit:~$ ")
	m.eatWinchNewline = true

	next, _ := m.Update(outputMsg{id: m.id, data: []byte("\nbandit0@bandit:~$ ")})
	m = next.(Model)

	got0 := strings.TrimRight(lineText(t, m.vt, 0, 20), " ")
	if got0 != "bandit0@bandit:~$" {
		t.Errorf("row 0 = %q, want the prompt overwritten in place", got0)
	}
	got1 := strings.TrimRight(lineText(t, m.vt, 1, 20), " ")
	if got1 != "" {
		t.Errorf("row 1 = %q, want blank (no extra prompt from a SIGWINCH newline)", got1)
	}
}

func TestOutputMsgClearsResizeSnap(t *testing.T) {
	m := newSizedModel(t, 10, 3, "0123456789")
	m, _ = m.SetSize(5, 3)
	if m.resizeSnap == nil {
		t.Fatal("expected a resize snapshot after shrink")
	}

	next, _ := m.Update(outputMsg{id: m.id, data: []byte("x")})
	m = next.(Model)
	if m.resizeSnap != nil {
		t.Fatal("outputMsg should drop the resize snapshot so new remote output wins")
	}
}

func TestReflowInvalidatedByWrite(t *testing.T) {
	m := newSizedModel(t, 10, 3, "0123456789")
	m, _ = m.SetSize(5, 3)

	// Same path Update takes on outputMsg: remote bytes replace the snapshot.
	m.vt.WriteString("\x1b[2J\x1b[Honly")
	m.resizeSnap = nil

	m, _ = m.SetSize(10, 3)
	got := lineText(t, m.vt, 0, 10)
	if got[:4] != "only" {
		t.Errorf("after a screen-clearing write, row 0 = %q, want it to start with %q", got, "only")
	}
	if got == "0123456789" {
		t.Errorf("grow after a write restored the pre-write snapshot; new output should win")
	}
}
