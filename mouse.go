package bubblessh

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// scrollFromWheel turns a wheel event into local scrolling. Only reached
// when mouse forwarding is off, so this is the "plain shell prompt"
// default rather than something that fights a remote program's own mouse
// handling — with WithMouseForwarding on, the wheel goes to the remote
// instead, see sendMouse.
func (m Model) scrollFromWheel(msg tea.MouseWheelMsg) Model {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		return m.ScrollUp(3)
	case tea.MouseWheelDown:
		return m.ScrollDown(3)
	}
	return m
}

// sendMouse forwards a mouse event to the remote program (e.g. vim/tmux
// with mouse mode enabled). Only active when the Model was built with
// WithMouseForwarding — most plain shell usage doesn't need this, and your
// top-level Bubble Tea program still has to request mouse tracking (via
// tea.View.MouseMode) for these messages to exist at all.
func (m Model) sendMouse(msg tea.MouseMsg) {
	if m.vt == nil {
		return
	}
	ev := msg.Mouse()
	button := uv.MouseButton(ev.Button)
	mod := uv.KeyMod(ev.Mod)

	switch msg.(type) {
	case tea.MouseClickMsg:
		m.vt.SendMouse(uv.MouseClickEvent{X: ev.X, Y: ev.Y, Button: button, Mod: mod})
	case tea.MouseReleaseMsg:
		m.vt.SendMouse(uv.MouseReleaseEvent{X: ev.X, Y: ev.Y, Button: button, Mod: mod})
	case tea.MouseMotionMsg:
		m.vt.SendMouse(uv.MouseMotionEvent{X: ev.X, Y: ev.Y, Button: button, Mod: mod})
	case tea.MouseWheelMsg:
		m.vt.SendMouse(uv.MouseWheelEvent{X: ev.X, Y: ev.Y, Button: button, Mod: mod})
	}
}
