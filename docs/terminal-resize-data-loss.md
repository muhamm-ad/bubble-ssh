# Terminal resize data loss (shrink, then grow back)

Status: local reflow ships in this repo (see [`resize.go`](../resize.go)). The proper fix still belongs in `charmbracelet/x/vt` / `charmbracelet/ultraviolet`, which have no wrap/reflow model. Upstream issue: <https://github.com/charmbracelet/ultraviolet/issues/180>.

## The problem

Shrinking an SSH pane permanently loses any screen content that falls outside the new size. Growing the pane back afterwards does not restore it, the pane just shows blank space where the text used to be.

### Reproduction

1. Run [`examples/split-pane`](../examples/split-pane) on a commit *without* (eg. <https://github.com/muhamm-ad/bubble-ssh/commit/2df8aae8a124d3951347083bd3a94adad9c7a070>) local reflow, connected to two hosts, with the window wide enough that a line of output isn't wrapped.
2. Shrink the terminal window (narrower and/or shorter).
3. Grow it back to the original size.
4. The line that got wrapped/cut on step 2 stays wrapped/cut, the characters that fell outside the smaller size never come back, even though there's room for them again.

## Root cause

Every SSH pane keeps its screen in a `*vt.Emulator` (`github.com/charmbracelet/x/vt`). `Model.SetSize` calls `vt.Emulator.Resize(cols, rows)`, which resizes the screen buffers it owns:

```go
// x/vt@.../emulator.go
func (e *Emulator) Resize(width int, height int) {
    ...
    e.scrs[0].Resize(width, height)
    e.scrs[1].Resize(width, height)
    ...
}
```

which forwards straight to `ultraviolet.Buffer.Resize`:

```go
// ultraviolet@.../buffer.go
func (b *Buffer) Resize(width int, height int) {
    ...
    } else if width < curWidth {
        for i := range b.Lines {
            b.Lines[i] = b.Lines[i][:width] // cells beyond width: gone
        }
    }
    ...
    } else if height < len(b.Lines) {
        b.Lines = b.Lines[:height] // rows beyond height: gone
    }
}
```

This resize is **destructive**:

- Shrinking width truncates every row to the new width. The truncated cells aren't wrapped onto a new row and aren't pushed to scrollback they're just discarded.
- Shrinking height keeps rows `[0, newHeight)` and drops everything from `newHeight` onward. Note this drops the *bottom* rows, not the top ones a terminal would normally scroll into history first.
- Growing back only appends blank cells to existing rows, or blank rows at the bottom. There is nothing left anywhere to restore from.

Verified this is still the case in the latest available commit of both modules as of 2026-09-07 (`x/vt@3986e9119cf9`, `ultraviolet@0277a179edd9`); not something already fixed upstream that we just need to bump a dependency to get.

Terminal emulators that support "reflow" (Windows Terminal, iTerm2, kitty) recompute line wrapping from a logical-line model on resize, in both directions. `ultraviolet.Buffer` has no such model, it's a plain fixed grid of cells which is what makes this resize destructive instead of lossy-but-recoverable.

## Local mitigation (implemented here)

[`resize.go`](../resize.go) wraps `vt.Resize` in `SetSize`:

1. **Snapshot** the live screen as logical lines (rows that fill their last column are treated as soft-wrapped continuations).
2. Call the destructive `vt.Resize`.
3. **Paint** a wrap of those logical lines back onto the emulator at the new size.

That means:

- **Width shrink**: the rest of the line wraps onto the next row instead of being discarded (if the pane is tall enough to show it).
- **Width grow**: those fragments unwrap back into the original line.
- **Height shrink/grow**: the snapshot still has the dropped rows, so growing the pane brings them back into the live view.

The snapshot is kept for the whole shrink/grow gesture and cleared when new bytes arrive from the remote (`outputMsg`). After that, the remote output is the source of truth — we must not paint stale cells over a prompt redraw or a full-screen app update.

The alternate screen (vim, htop, …) is left alone. Those apps redraw themselves on the SSH window-change; reflowing their grid would fight that redraw.

### Why window-change is debounced

`SetSize` still notifies the remote PTY, but it waits `windowChangeDebounce` (50ms) so a drag doesn't send SIGWINCH on every pixel.
A burst of window-changes makes the remote shell reprint the prompt, which would drop the snapshot while the user is still resizing and leave the live view cropped again.

### Limits (why upstream still matters)

- There is no real wrap-flag on `ultraviolet` cells. Soft-wrap is inferred ("last column is non-blank"), which can join two lines that happened to fill the width and then had a hard newline.
- New remote output at the smaller size replaces the snapshot. Growing after that will not resurrect cells the remote itself has overwritten.
- Scrollback history is not reflowed, only the live screen.
- This is still a sidecar around a destructive `Buffer.Resize`. The durable fix is reflow inside `ultraviolet`.

## Upstream

The actual fix (reflow support in `Buffer.Resize`, in both directions) belongs in `charmbracelet/ultraviolet`, since `x/vt` and therefore bubble-ssh both sit on top of it. Tracked in [`TODO.md`](../TODO.md).
