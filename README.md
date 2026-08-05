# bubble-ssh

An interactive SSH terminal as a [Bubble Tea v2](https://charm.land/bubbletea/v2) component. `bubblessh.Model` dials an SSH server, requests a PTY, starts a shell, and renders it exactly like a normal `ssh` session would — keystrokes go to the remote shell, remote output (colors, cursor movement, full-screen apps like `vim`/`htop`) is interpreted and rendered back. Drop it in as your whole program, or embed it as one pane among several.

```go
import "github.com/muhamm-ad/bubblessh"

m := bubblessh.New("example.com:22",
    bubblessh.WithUser("alice"),
    bubblessh.WithAgent(),        // or WithPassword(...) / WithPrivateKeyFile(...)
    bubblessh.WithSize(80, 24),
    bubblessh.WithKnownHostsFile("~/.ssh/known_hosts"),
)

p := tea.NewProgram(m)
p.Run()
```

## Install

```bash
go get github.com/muhamm-ad/bubblessh
```

## Why this needed writing (and what it's built on)

As of August 2026, we didn't find a single off-the-shelf "SSH pane for Bubble Tea" package. `bubblessh` wires together three libraries that each do one part well:

| Library | Role |
| --- | --- |
| [`golang.org/x/crypto/ssh`](https://pkg.go.dev/golang.org/x/crypto/ssh) | dials the server, authenticates, opens the PTY + shell |
| [`github.com/charmbracelet/x/vt`](https://pkg.go.dev/github.com/charmbracelet/x/vt) | a full VT220/ANSI terminal emulator — turns the remote byte stream into a screen you can render, and turns key/mouse events back into the right escape sequences |
| [`charm.land/bubbletea/v2`](https://charm.land/bubbletea/v2) | the Elm-architecture event loop that ties it into your TUI |

See [doc.go](./doc.go) for an architecture diagram of how data flows between them.

## Usage patterns

### As the whole program

See [`examples/basic`](./examples/basic) — wraps `bubblessh.Model` in a tiny root model that handles quitting on Ctrl+C and forwards `tea.WindowSizeMsg` into `SetSize`. `bubblessh.Model` deliberately never calls `tea.Quit` itself or auto-tracks the window size, since it's also meant to be embedded — those are decisions for whatever owns the top-level program.

### Embedded as one pane among several

See [`examples/split-pane`](./examples/split-pane) — two independent SSH sessions rendered side by side with [lipgloss](https://charm.land/lipgloss/v2), Tab to switch which one receives keystrokes. Every `bubblessh.Model` tags its internal async messages with its own instance id, so it's safe to `Update()` several instances with the same incoming message — each one ignores messages that aren't its own. Use `Content()` (a plain ANSI string) rather than `View()` (a `tea.View`) when composing a pane into a bigger layout.

## Options

| Option | Purpose |
| --- | --- |
| `WithUser(user)` | SSH username (required) |
| `WithPassword(pw)` | password auth |
| `WithPrivateKey(pem, passphrase)` | key auth from raw PEM bytes |
| `WithPrivateKeyFile(path, passphrase)` | key auth, read from disk |
| `WithAgent()` | auth via `ssh-agent` (`SSH_AUTH_SOCK`) |
| `WithPort(n)` | override the port |
| `WithKnownHostsFile(paths...)` | verify the host key against `known_hosts` file(s), reject unknown hosts |
| `WithAcceptNewHostKeys(path)` | trust a new host once, remember it, verify strictly after that (`ssh -o StrictHostKeyChecking=accept-new`) |
| `WithInsecureIgnoreHostKey()` | **disables host key verification** — testing/localhost only |
| `WithSize(cols, rows)` | initial PTY size (default 80x24) |
| `WithTerm(term)` | `TERM` sent to the remote PTY (default `xterm-256color`) |
| `WithEnv(k, v)` | request an env var (subject to the server's `AcceptEnv`) |
| `WithMouseForwarding()` | forward mouse events to the remote program |
| `WithConnectTimeout(d)` | dial/auth timeout (default 10s) |

You can combine multiple auth options — they're tried in the order given, same as the underlying `ssh` package.

Several auth methods (password, key, agent) can be combined; the client tries each `ssh.AuthMethod` in order. Multiple calls to `WithPassword`/`WithPrivateKey*`/`WithAgent` all just append to that list.

### Host key verification

If you don't call `WithKnownHostsFile`, `WithAcceptNewHostKeys`, or `WithInsecureIgnoreHostKey`, `bubblessh` tries `~/.ssh/known_hosts` and returns a clear error if it can't find it — it will **not** silently skip verification. This is a deliberate "fail closed" default. For a real host you'll reconnect to, `WithAcceptNewHostKeys(path)` is the closest equivalent to the interactive "are you sure you want to continue connecting?" prompt a normal `ssh` client shows (bubblessh can't show that prompt itself — `Connect` runs on a background goroutine while Bubble Tea already owns the terminal — so it trades the prompt for automatic, remembered trust instead). Reach for `WithInsecureIgnoreHostKey()` only where MITM risk genuinely doesn't matter, e.g. a local container — it trusts every connection, forever, with no memory of anything.

## A note on the dependency versions

This was written in July 2026, right after Bubble Tea shipped a stable v2 (new module path `charm.land/bubbletea/v2`, rebuilt on `charmbracelet/ultraviolet` primitives — `Model.View()` now returns a `tea.View` instead of a plain `string`, and `KeyMsg` is an interface implemented by `KeyPressMsg`/`KeyReleaseMsg`). `charmbracelet/x/vt` is explicitly labeled experimental by Charm, so its API can still move. Every method/type this package calls was checked against the actual upstream source at the time of writing, but if `go build` complains after you `go mod tidy`, it's most likely a small rename in `x/vt` or `ultraviolet`; the fix is almost always a one-line adjustment in `keys.go`, `mouse.go`, or `connect.go`.

## License

MIT, see [LICENSE](./LICENSE).
