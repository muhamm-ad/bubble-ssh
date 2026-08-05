# ssh-agent support across platforms

Status: known limitation, documented, not yet fixed. See [`TODO.md`](../TODO.md).

## The problem

`WithAgent()` connects to the running `ssh-agent` with:

```go
conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
signers, err := agent.NewClient(conn).Signers()
```

This assumes the agent exposes itself as a **Unix domain socket** — a file
on disk that `net.Dial("unix", path)` can connect to. That's true on macOS
and Linux, and it's true in POSIX-emulating environments on Windows (Git
Bash/MSYS2, WSL). It is **not** true for the agent most Windows users
actually have running: the native `ssh-agent` Windows service, which
communicates over a **named pipe** (`\\.\pipe\openssh-ssh-agent`) instead.

Confirmed directly in the upstream test suite —
`golang.org/x/crypto/ssh/agent` skips this exact scenario:

```go
// client_test.go
if runtime.GOOS == "windows" {
    t.Skip("skipping on windows, we don't support connecting to the ssh-agent via a named pipe")
}
```

The package we depend on for `agent.NewClient(...).Signers()` was never
built to reach a named pipe. `net.Dial("unix", ...)` and a named pipe are
two different Windows IPC primitives (`AF_UNIX` socket vs. `CreateFile` on
a `\\.\pipe\...` path) — there's no flag or workaround inside `net.Dial`
that bridges the two.

## How the agent's transport differs per platform

| Platform | Agent transport | `SSH_AUTH_SOCK` set? | Our current `WithAgent()` |
| --- | --- | --- | --- |
| macOS | Unix domain socket | yes (system agent, Keychain-integrated) | ✅ works |
| Linux | Unix domain socket | yes, if an agent is running | ✅ works |
| Windows — Git Bash / MSYS2 | Unix domain socket (emulated) | yes | ✅ likely works |
| Windows — WSL | Unix domain socket (it's Linux) | yes, if run from inside WSL | ✅ works |
| Windows — native OpenSSH service | **named pipe** `\\.\pipe\openssh-ssh-agent` | not by default | ❌ fails |
| Windows — Pageant (PuTTY) | Windows messages (`WM_COPYDATA`), not a socket or pipe at all | no | ❌ fails, different protocol entirely |

## How to start the agent, per platform (for testing)

**PowerShell (native Windows service):**

```powershell
Get-Service ssh-agent
Set-Service -Name ssh-agent -StartupType Automatic   # once, elevated
Start-Service ssh-agent
ssh-add $env:USERPROFILE\.ssh\id_ed25519
```

**bash/zsh (macOS, Linux, Git Bash):**

```bash
eval $(ssh-agent -s)   # eval is required: ssh-agent -s only PRINTS the
                        # export statements, a child process can't set
                        # env vars in its parent shell on its own
ssh-add ~/.ssh/id_ed25519
```

## Solution options

### A. Do nothing (current state)

Document the gap (this file + `TODO.md`), let `WithAgent()` fail with the
existing clear error (`SSH_AUTH_SOCK is not set, is ssh-agent running?`) on
native Windows. Users on native Windows fall back to `WithPrivateKeyFile`
or run the program from Git Bash/WSL.

- Cost: none.
- Downside: `WithAgent()` silently doesn't do what a Windows user expects,
  unless they've read this doc.

### B. Detect Windows, dial the named pipe directly

Add a build-tagged file (`agent_windows.go`) using
[`github.com/Microsoft/go-winio`](https://github.com/microsoft/go-winio)
(the same library Docker and Kubernetes use for Windows named-pipe IPC on
Windows):

```go
//go:build windows

func dialAgent() (net.Conn, error) {
    if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
        return net.Dial("unix", sock) // Git Bash/MSYS2/WSL case
    }
    return winio.DialPipe(`\\.\pipe\openssh-ssh-agent`, nil)
}
```

paired with a non-Windows `agent_other.go` (`//go:build !windows`) keeping
today's `net.Dial("unix", ...)`. `WithAgent()` calls `dialAgent()` instead
of hardcoding the network type.

- Cost: one new dependency (`go-winio`), two small platform-specific files.
- Covers: the native Windows OpenSSH agent service — the common case for
  Windows users who haven't set up Git Bash or WSL specifically for this.
- Doesn't cover: Pageant (different protocol, see C).

### C. Also support Pageant (PuTTY's agent)

Pageant doesn't use sockets or pipes — it uses a Windows-specific IPC
mechanism (`WM_COPYDATA` messages to a hidden window). Supporting it means
implementing Pageant's wire protocol from scratch, or pulling in a
third-party package that already has
(e.g. some forks of `golang.org/x/crypto/ssh/agent` add this; nothing
official does as of this writing).

- Cost: meaningfully higher than B — a different protocol, not just a
  different transport.
- Worth it only if actual users show up wanting Pageant specifically
  (common among long-time PuTTY users, less so for anyone who arrived at
  Windows OpenSSH more recently).

### D. Side-step the problem: in-process agent instead of connecting to one

Not a fix for `WithAgent()` itself, but worth knowing about: the same
`golang.org/x/crypto/ssh/agent` package includes `agent.NewKeyring()` — a
full agent implementation that runs **inside your own process**, no socket
or pipe involved at all:

```go
kr := agent.NewKeyring()
kr.Add(agent.AddedKey{PrivateKey: someKey})
signers, _ := kr.Signers()
```

Useful for generating or holding a short-lived key at runtime and using it
immediately without writing it to disk or sharing it with other tools on
the machine — e.g. a CI job that pulls a key from a secret manager for the
duration of one pipeline run. Doesn't help a user who specifically wants to
reuse their already-running system agent, which is what `WithAgent()` is
for.

<!-- ## Recommendation

Option B covers the actual common case (Windows users on the built-in
OpenSSH agent) for a small, well-scoped addition. Option C is a "wait and
see if anyone asks" — Pageant users are a shrinking minority now that
Windows ships OpenSSH out of the box. Not implementing either yet; revisit
when there's a concrete Windows user hitting this. -->
