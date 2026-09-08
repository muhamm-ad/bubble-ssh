package bubblessh

import (
	"bytes"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// resizeSnap holds the live screen as logical (unwrapped) lines, captured
// before a shrink/grow sequence. ultraviolet's Buffer.Resize is destructive,
// so we keep this sidecar and paint a wrap of it back onto the emulator after
// every SetSize until new remote output arrives (which invalidates it).
// See docs/terminal-resize-data-loss.md.
type resizeSnap struct {
	lines [][]uv.Cell
}

func captureResizeSnap(term *vt.Emulator) *resizeSnap {
	lines := joinSoftWrapped(snapshotGrid(term), term.Width())
	return &resizeSnap{lines: trimTrailingEmptyLines(lines)}
}

func trimTrailingEmptyLines(lines [][]uv.Cell) [][]uv.Cell {
	i := len(lines)
	for i > 0 {
		if len(trimTrailingCells(lines[i-1])) > 0 {
			break
		}
		i--
	}
	return lines[:i]
}

func snapshotGrid(term *vt.Emulator) [][]uv.Cell {
	cols, rows := term.Width(), term.Height()
	grid := make([][]uv.Cell, rows)
	for y := 0; y < rows; y++ {
		row := make([]uv.Cell, cols)
		for x := 0; x < cols; x++ {
			if c := term.CellAt(x, y); c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		grid[y] = row
	}
	return grid
}

// joinSoftWrapped turns a fixed-width grid into logical lines. A row that
// occupies its last column is treated as a soft-wrap continuation of the next
// row — the same heuristic terminals use when they don't store a wrap flag.
func joinSoftWrapped(grid [][]uv.Cell, width int) [][]uv.Cell {
	var out [][]uv.Cell
	var cur []uv.Cell
	continued := false
	for _, row := range grid {
		copied := append([]uv.Cell(nil), row...)
		if continued && len(cur) > 0 {
			cur = append(cur, copied...)
		} else {
			if cur != nil {
				out = append(out, trimTrailingCells(cur))
			}
			cur = copied
		}
		continued = rowContinues(row, width)
	}
	if cur != nil {
		out = append(out, trimTrailingCells(cur))
	}
	return out
}

func rowContinues(row []uv.Cell, width int) bool {
	if width < 1 || len(row) < width {
		return false
	}
	last := row[width-1]
	if last.Width == 0 {
		// Continuation column of a wide cell — the row is full.
		return true
	}
	return !last.IsZero() && !isBlankCell(&last)
}

func trimTrailingCells(row []uv.Cell) []uv.Cell {
	i := len(row)
	for i > 0 {
		c := &row[i-1]
		if c.Width != 0 && !c.IsZero() && !isBlankCell(c) {
			break
		}
		i--
	}
	return row[:i]
}

func wrapLogical(lines [][]uv.Cell, width int) [][]uv.Cell {
	if width < 1 {
		width = 1
	}
	var out [][]uv.Cell
	for _, line := range lines {
		out = append(out, wrapLine(line, width)...)
	}
	if len(out) == 0 {
		return [][]uv.Cell{nil}
	}
	return out
}

func wrapLine(line []uv.Cell, width int) [][]uv.Cell {
	if len(line) == 0 {
		return [][]uv.Cell{nil}
	}
	var rows [][]uv.Cell
	var cur []uv.Cell
	x := 0
	for i := range line {
		c := line[i]
		if c.Width == 0 {
			continue
		}
		w := c.Width
		if w < 1 {
			w = 1
		}
		if x+w > width && len(cur) > 0 {
			rows = append(rows, cur)
			cur = nil
			x = 0
		}
		cur = append(cur, c)
		x += w
	}
	if cur != nil || len(rows) == 0 {
		rows = append(rows, cur)
	}
	return rows
}

// paintSnap wraps the captured logical lines to the new size and writes them
// back onto the emulator. Rows that don't fit are dropped from the top so the
// bottom of the buffer (the prompt) stays visible; growing brings them back
// because we still have the original logical lines.
func paintSnap(term *vt.Emulator, snap *resizeSnap, cols, rows int) {
	visual := wrapLogical(snap.lines, cols)
	if len(visual) > rows {
		visual = visual[len(visual)-rows:]
	}

	blank := uv.EmptyCell
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			term.SetCell(x, y, &blank)
		}
	}
	for y, row := range visual {
		x := 0
		for i := range row {
			c := row[i]
			if c.Width == 0 {
				continue
			}
			w := c.Width
			if w < 1 {
				w = 1
			}
			if x >= cols {
				break
			}
			term.SetCell(x, y, &c)
			x += w
		}
	}
	// Put the cursor on the prompt line so a later SIGWINCH redraw
	// overwrites that line instead of printing a new one below it.
	syncCursor(term, visual, cols, rows)
}

// lastContent reports the last non-blank cell's next column and row in visual.
func lastContent(visual [][]uv.Cell, cols, rows int) (x, y int) {
	for rowIdx, row := range visual {
		if rowIdx >= rows {
			break
		}
		col := 0
		found := false
		end := 0
		for i := range row {
			c := row[i]
			if c.Width == 0 {
				continue
			}
			w := c.Width
			if w < 1 {
				w = 1
			}
			if !c.IsZero() && !isBlankCell(&c) {
				found = true
				end = col + w
			}
			col += w
		}
		if found {
			y = rowIdx
			x = end
			if x > cols {
				x = cols
			}
		}
	}
	return x, y
}

func syncCursor(term *vt.Emulator, visual [][]uv.Cell, cols, rows int) {
	x, y := lastContent(visual, cols, rows)
	if x >= cols {
		x = cols - 1
	}
	if x < 0 {
		x = 0
	}
	_, _ = term.WriteString(ansi.CursorPosition(x+1, y+1))
}

func moveCursorToLineStart(term *vt.Emulator, y int) {
	if y < 0 {
		y = 0
	}
	_, _ = term.WriteString(ansi.CursorPosition(1, y+1))
}

// stripLeadingCRLF removes a single leading CR/LF that bash/readline often
// emits on SIGWINCH before reprinting the prompt. That looks like the user
// pressed Enter after every resize.
func stripLeadingCRLF(b []byte) []byte {
	switch {
	case bytes.HasPrefix(b, []byte("\r\n")):
		return b[2:]
	case bytes.HasPrefix(b, []byte("\n")):
		return b[1:]
	case bytes.HasPrefix(b, []byte("\r")):
		return b[1:]
	default:
		return b
	}
}

func isBlankCell(c *uv.Cell) bool {
	return c.Content == "" || c.Content == " "
}

// reflowAfterResize captures overflow that Buffer.Resize is about to destroy
// (or already would have, if we paint after), then puts a wrapped copy of the
// captured screen back into the live view.
func (m Model) reflowAfterResize(cols, rows int) Model {
	if m.vt == nil {
		return m
	}
	if m.vt.IsAltScreen() {
		// Full-screen remote apps redraw themselves on the window-change
		// we send; reflowing their grid would fight that redraw.
		m.vt.Resize(cols, rows)
		return m
	}
	if m.resizeSnap == nil {
		m.resizeSnap = captureResizeSnap(m.vt)
	}
	m.vt.Resize(cols, rows)
	paintSnap(m.vt, m.resizeSnap, cols, rows)
	return m
}
