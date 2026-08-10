package bubblessh

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ScrollUp scrolls the view up by the given number of lines, into the
// scrollback history. Clamped at the oldest available line.
func (m Model) ScrollUp(lines int) Model {
	if m.vt == nil {
		return m
	}
	m.scrollOffset += lines
	if max := m.vt.ScrollbackLen(); m.scrollOffset > max {
		m.scrollOffset = max
	}
	return m
}

// ScrollDown scrolls the view down by the given number of lines, back
// toward the live screen.
func (m Model) ScrollDown(lines int) Model {
	m.scrollOffset -= lines
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	return m
}

// ScrollToBottom returns to the live view. A normal terminal does this the
// moment you type anything while scrolled back — Update() already calls
// this on every key press and paste, so you're never typing blind into a
// view that's showing history instead of the live screen.
func (m Model) ScrollToBottom() Model {
	m.scrollOffset = 0
	return m
}

// Scrolled reports whether the view is currently showing scrollback
// history instead of the live screen.
func (m Model) Scrolled() bool {
	return m.scrollOffset > 0
}

// renderScrolled composes scrollback and live-screen rows into a single
// buffer for the current scroll offset, and renders it through the same
// engine Content() uses for the live view — same colors and styling, just
// a different window into the combined history.
func (m Model) renderScrolled() string {
	width, height := m.vt.Width(), m.vt.Height()
	n := m.vt.ScrollbackLen()
	offset := m.scrollOffset
	if offset > n {
		offset = n
	}

	buf := uv.NewBuffer(width, height)
	for row := 0; row < height; row++ {
		combined := n - offset + row
		for x := 0; x < width; x++ {
			var cell *uv.Cell
			if combined < n {
				cell = m.vt.ScrollbackCellAt(x, combined)
			} else {
				cell = m.vt.CellAt(x, combined-n)
			}
			if cell != nil {
				buf.SetCell(x, row, cell)
			}
		}
	}
	return buf.Render()
}

// fitBlock clamps s to exactly rows lines of at most cols cells each.
// When s is taller than rows, the bottom-most lines are kept (most recent
// terminal output). Shorter content is padded with blank lines at the
// bottom so a parent layout can size a border around a fixed pane.
func fitBlock(s string, cols, rows int) string {
	if rows < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	} else {
		for len(lines) < rows {
			lines = append(lines, "")
		}
	}
	if cols > 0 {
		for i, line := range lines {
			if ansi.StringWidth(line) > cols {
				lines[i] = ansi.Truncate(line, cols, "")
			}
		}
	}
	return strings.Join(lines, "\n")
}
