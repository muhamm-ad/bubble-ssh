package bubble_ssh

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

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
	m.vt.SendKey(uv.KeyPressEvent{
		Text:        k.Text,
		Mod:         uv.KeyMod(k.Mod),
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		BaseCode:    k.BaseCode,
		IsRepeat:    k.IsRepeat,
	})
}
