# TODO

## Terminal resize permanently drops content (upstream)

Shrinking an SSH pane crops the local `vt.Emulator` screen buffer; content past the new size is discarded, not reflowed or saved. Growing the pane back afterwards doesn't restore it, root cause is `ultraviolet.Buffer.Resize` truncating/dropping cells with no reflow, in `charmbracelet/ultraviolet` (inherited via `charmbracelet/x/vt`), confirmed still present in the latest available commit of both as of 2026-09-07.

A local reflow ships in `resize.go`: the live screen is snapshotted as logical lines before the destructive `vt.Resize`, then wrapped back onto the emulator, so shrinking and growing restores the cropped characters in the live view (not only in scrollback). Alternate screen is left to the remote app. Window-change is debounced so a drag doesn't drop the snapshot.

Full writeup: [`docs/terminal-resize-data-loss.md`](./docs/terminal-resize-data-loss.md).

Upstream issue: <https://github.com/charmbracelet/ultraviolet/issues/180>.

## `WithAgent()` doesn't work on native Windows

`net.Dial("unix", sock)` can't reach the Windows OpenSSH agent service, it talks over a named pipe (`\\.\pipe\openssh-ssh-agent`), not a Unix domain socket. Confirmed straight from `golang.org/x/crypto/ssh/agent`'s own test suite, which skips this exact case on `GOOS=windows`.

Works today: Git Bash/MSYS2 and WSL (both expose a real Unix socket).
Doesn't work today: PowerShell/cmd with the native `ssh-agent` Windows service.

Full writeup + solution options: [`docs/ssh-agent-windows.md`](./docs/ssh-agent-windows.md).

Not fixing yet, current code stays as-is until we pick an approach.
