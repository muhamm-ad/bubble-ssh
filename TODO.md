# TODO

## `WithAgent()` doesn't work on native Windows

`net.Dial("unix", sock)` can't reach the Windows OpenSSH agent service — it
talks over a named pipe (`\\.\pipe\openssh-ssh-agent`), not a Unix domain
socket. Confirmed straight from `golang.org/x/crypto/ssh/agent`'s own test
suite, which skips this exact case on `GOOS=windows`.

Works today: Git Bash/MSYS2 and WSL (both expose a real Unix socket).
Doesn't work today: PowerShell/cmd with the native `ssh-agent` Windows
service.

Full writeup + solution options: [`docs/ssh-agent-windows.md`](./docs/ssh-agent-windows.md).

Not fixing yet — current code stays as-is until we pick an approach.
