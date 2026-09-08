package bubblessh

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// lockMods are keyboard lock-state flags, not chords. Windows in particular
// often reports NumLock alongside ordinary letter keys.
const lockMods = uv.ModShift | uv.ModCapsLock | uv.ModNumLock | uv.ModScrollLock

// sendKey converts a Bubble Tea key event into the ultraviolet key event
// type the vt.Emulator expects, and lets the emulator do the actual VT100/
// xterm encoding (including cursor-key application-mode, etc.) — the
// encoded bytes come back out through the emulator's Read()/InputPipe(),
// which the io.Copy goroutine started in connect() forwards to the remote
// shell's stdin.
//
// tea.Key and uv.Key are structurally identical (same fields, same KeyMod
// type), but we convert field-by-field rather than relying on a raw type
// conversion so this keeps compiling even if a future release reorders
// fields or adds new ones.
func (m Model) sendKey(msg tea.KeyPressMsg) {
	if m.vt == nil {
		return
	}
	k := msg.Key()
	// vt.SendKey only emits a printable rune when Mod == 0, so Shift/CapsLock
	// letters ("A") and shifted symbols ("!") are dropped. When the event
	// already carries the produced text and isn't a ctrl/alt chord, send that
	// text straight to the PTY.
	if text := printableKeyText(k); text != "" {
		m.vt.SendText(text)
		return
	}
	m.vt.SendKey(uv.KeyPressEvent{
		Text:        k.Text,
		Mod:         uv.KeyMod(k.Mod),
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		BaseCode:    k.BaseCode,
		IsRepeat:    k.IsRepeat,
	})
}

// printableKeyText is the characters a key should type into the remote PTY.
// Empty for special keys (enter, arrows) and for real chords (ctrl/alt/…).
func printableKeyText(k tea.Key) string {
	if k.Text == "" {
		return ""
	}
	if uv.KeyMod(k.Mod)&^lockMods != 0 {
		return ""
	}
	return k.Text
}
