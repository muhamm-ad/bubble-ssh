package bubblessh

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
)

func TestSendKeyShiftLetter(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Text: "A", Mod: tea.ModShift, Code: 'a'})
	if got != "A" {
		t.Fatalf("shift+a = %q, want \"A\"", got)
	}
}

func TestSendKeyCapsLockLetter(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Text: "A", Mod: tea.ModCapsLock, Code: 'a'})
	if got != "A" {
		t.Fatalf("caps lock a = %q, want \"A\"", got)
	}
}

func TestSendKeyShiftSymbol(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Text: "!", Mod: tea.ModShift, Code: '1'})
	if got != "!" {
		t.Fatalf("shift+1 = %q, want \"!\"", got)
	}
}

func TestSendKeyLowercase(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Text: "a", Code: 'a'})
	if got != "a" {
		t.Fatalf("a = %q, want \"a\"", got)
	}
}

func TestSendKeyCtrlC(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'})
	if got != "\x03" {
		t.Fatalf("ctrl+c = %q, want \\x03", got)
	}
}

func TestSendKeyEnter(t *testing.T) {
	got := ptyBytes(t, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got != "\r" {
		t.Fatalf("enter = %q, want \\r", got)
	}
}

func TestPrintableKeyTextSkipsChords(t *testing.T) {
	if got := printableKeyText(tea.Key{Text: "a", Mod: tea.ModAlt, Code: 'a'}); got != "" {
		t.Fatalf("alt+a text = %q, want empty so SendKey can prefix ESC", got)
	}
}

func ptyBytes(t *testing.T, msg tea.KeyPressMsg) string {
	t.Helper()
	term := vt.NewEmulator(80, 24)
	t.Cleanup(func() { _ = term.Close() })

	ch := make(chan string, 1)
	go func() {
		buf := make([]byte, 32)
		n, err := term.Read(buf)
		if err != nil {
			ch <- ""
			return
		}
		ch <- string(buf[:n])
	}()

	Model{vt: term}.sendKey(msg)

	select {
	case got := <-ch:
		return got
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bytes on the emulator input pipe")
		return ""
	}
}
