package bubblessh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Option configures a Model. Pass any number of them to New.
type Option func(*Model)

// WithUser sets the SSH username. Required.
func WithUser(user string) Option {
	return func(m *Model) { m.user = user }
}

// WithPassword adds password authentication. You can combine this with
// other auth options (e.g. WithAgent) — the SSH client tries each in turn.
func WithPassword(password string) Option {
	return func(m *Model) {
		m.authMethods = append(m.authMethods, ssh.Password(password))
	}
}

// WithPrivateKey adds public-key authentication from raw PEM bytes. Pass an
// empty passphrase if the key isn't encrypted.
func WithPrivateKey(pemBytes []byte, passphrase string) Option {
	return func(m *Model) {
		signer, err := parsePEM(pemBytes, passphrase)
		if err != nil {
			m.setupErr = fmt.Errorf("bubblessh: parsing private key: %w", err)
			return
		}
		m.authMethods = append(m.authMethods, ssh.PublicKeys(signer))
	}
}

// WithPrivateKeyFile adds public-key authentication, reading the key from
// disk (e.g. "~/.ssh/id_ed25519" — expand "~" yourself, Go doesn't). Pass an
// empty passphrase if the key isn't encrypted.
func WithPrivateKeyFile(path, passphrase string) Option {
	return func(m *Model) {
		pemBytes, err := os.ReadFile(path)
		if err != nil {
			m.setupErr = fmt.Errorf("bubblessh: reading private key %s: %w", path, err)
			return
		}
		signer, err := parsePEM(pemBytes, passphrase)
		if err != nil {
			m.setupErr = fmt.Errorf("bubblessh: parsing private key %s: %w", path, err)
			return
		}
		m.authMethods = append(m.authMethods, ssh.PublicKeys(signer))
	}
}

func parsePEM(pemBytes []byte, passphrase string) (ssh.Signer, error) {
	if passphrase == "" {
		return ssh.ParsePrivateKey(pemBytes)
	}
	return ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
}

// SSH_AUTH_SOCK environment variable. It's resolved lazily at connection
// time, so it's safe to call even if no agent is running yet (it'll just
// fail at Connect time with a clear error).
//
// Windows: this only reaches agents exposed as a Unix domain socket
// (macOS, Linux, Git Bash/MSYS2, WSL). The native Windows OpenSSH agent
// service uses a named pipe instead, which net.Dial("unix", ...) can't
// reach — see docs/ssh-agent-windows.md for the full explanation and
// possible fixes. Not fixed yet; documented as a known gap.
func WithAgent() Option {
	return func(m *Model) {
		m.authMethods = append(m.authMethods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			sock := os.Getenv("SSH_AUTH_SOCK")
			if sock == "" {
				return nil, fmt.Errorf("bubblessh: SSH_AUTH_SOCK is not set, is ssh-agent running?")
			}
			conn, err := net.Dial("unix", sock)
			if err != nil {
				return nil, fmt.Errorf("bubblessh: connecting to ssh-agent: %w", err)
			}
			return agent.NewClient(conn).Signers()
		}))
	}
}

// WithKnownHostsFile verifies the server's host key against one or more
// OpenSSH-format known_hosts files (e.g. "~/.ssh/known_hosts", expanded by
// you). Unknown hosts are rejected — this never prompts or writes anything,
// it only checks. If you want a new host to be trusted automatically on
// first connection and remembered from then on, use
// WithAcceptNewHostKeys instead. If you never call this, WithAcceptNewHostKeys,
// or WithInsecureIgnoreHostKey, Connect will try the default
// "~/.ssh/known_hosts" and fail loudly if it can't find it, rather than
// silently skipping verification.
func WithKnownHostsFile(paths ...string) Option {
	return func(m *Model) {
		cb, err := knownhosts.New(paths...)
		if err != nil {
			m.setupErr = fmt.Errorf("bubblessh: loading known_hosts: %w", err)
			return
		}
		m.hostKeyCallback = cb
	}
}

// WithAcceptNewHostKeys behaves like `ssh -o StrictHostKeyChecking=accept-new`:
// a host you've never connected to before is trusted automatically, and its
// key is appended to the known_hosts file at path (created if it doesn't
// exist yet, along with any missing parent directories). Every later
// connection to that host is then checked strictly against what was
// learned — if the server's key ever changes unexpectedly, the connection
// is refused. That refusal on change is the actual security property; the
// "trust" part only ever applies once, to a genuinely new host. This is the
// closest equivalent to the interactive "are you sure you want to continue
// connecting?" prompt a normal ssh client shows — bubble-ssh can't show
// that prompt itself (Connect runs on a background goroutine while Bubble
// Tea already owns the terminal), so this trades the prompt for automatic,
// remembered trust instead.
func WithAcceptNewHostKeys(path string) Option {
	return func(m *Model) {
		if err := ensureFileExists(path); err != nil {
			m.setupErr = fmt.Errorf("bubblessh: preparing known_hosts %s: %w", path, err)
			return
		}
		// Sanity check at setup time: fail fast on a malformed file rather
		// than only discovering it during the first connection attempt.
		if _, err := knownhosts.New(path); err != nil {
			m.setupErr = fmt.Errorf("bubblessh: loading known_hosts %s: %w", path, err)
			return
		}

		m.hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// Re-parse on every call: knownhosts.New() snapshots the file
			// once and never re-reads it, so a callback built ahead of
			// time would never notice entries we ourselves just wrote —
			// e.g. checking the same brand-new host twice in one run.
			strictCallback, err := knownhosts.New(path)
			if err != nil {
				return fmt.Errorf("bubblessh: re-loading known_hosts %s: %w", path, err)
			}

			err = strictCallback(hostname, remote, key)
			if err == nil {
				return nil // known host, key matches — the common case after the first time
			}

			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) {
				return err // some other error (e.g. a revoked key) — don't swallow it
			}
			if len(keyErr.Want) > 0 {
				// Want is non-empty: we DO have a stored key for this host,
				// and it doesn't match what the server just presented —
				// a real "someone might be impersonating the server"
				// signal. Refuse, exactly like strict mode would.
				return err
			}

			// Want is empty: genuinely never seen this host before. Trust
			// it once, and write it down so next time it's checked strictly.
			return appendKnownHost(path, hostname, key)
		}
	}
}
func ensureFileExists(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(knownhosts.Line([]string{hostname}, key) + "\n")
	return err
}

// WithInsecureIgnoreHostKey disables host key verification entirely — every
// connection, forever, no memory of anything. This makes the connection
// vulnerable to man-in-the-middle attacks — only use it for throwaway boxes
// (e.g. a container on localhost) or local testing. For a real host you'll
// reconnect to, prefer WithAcceptNewHostKeys: it only trusts blindly once,
// then verifies strictly from then on.
func WithInsecureIgnoreHostKey() Option {
	return func(m *Model) { m.hostKeyCallback = ssh.InsecureIgnoreHostKey() } //nolint:gosec
}

// WithPort overrides the port. If you already included a port in the addr
// passed to New (e.g. "host:2222"), this is not needed.
func WithPort(port int) Option {
	return func(m *Model) { m.port = port }
}

// WithSize sets the initial PTY size, in columns and rows. Default is 80x24.
// Call SetSize later to resize an already-connected session (e.g. in
// response to tea.WindowSizeMsg).
func WithSize(cols, rows int) Option {
	return func(m *Model) { m.width, m.height = cols, rows }
}

// WithTerm sets the TERM environment variable requested for the remote PTY.
// Default is "xterm-256color".
func WithTerm(term string) Option {
	return func(m *Model) { m.term = term }
}

// WithEnv requests an extra environment variable on the remote session.
// Note most sshd configs only forward variables listed in their AcceptEnv
// directive — this is a server-side restriction bubblessh can't work around.
func WithEnv(key, value string) Option {
	return func(m *Model) {
		if m.env == nil {
			m.env = map[string]string{}
		}
		m.env[key] = value
	}
}

// WithMouseForwarding forwards mouse events (clicks, wheel, motion) to the
// remote program, useful for full-screen remote apps like vim or tmux with
// mouse mode on. Your top-level tea.Program / parent View still needs to
// request mouse tracking (set tea.View.MouseMode) for Bubble Tea to emit
// mouse messages in the first place.
func WithMouseForwarding() Option {
	return func(m *Model) { m.mouseForwarding = true }
}

// WithConnectTimeout bounds how long dialing and authentication may take.
// Default is 10 seconds.
func WithConnectTimeout(d time.Duration) Option {
	return func(m *Model) { m.connectTimeout = d }
}

type CursorShape int

const (
	CursorBlock CursorShape = iota
	CursorUnderline
	CursorBar
)

// WithCursorShape sets how the connected terminal's cursor is drawn.
// Default is CursorBar.
//
// This is a fixed choice — the cursor always renders in this shape,
// regardless of what the remote program is doing. It does not track
// cursor-shape requests from the remote side.
// bubblessh always shows the one shape you pick here.
func WithCursorShape(shape CursorShape) Option {
	return func(m *Model) { m.cursorShape = shape }
}
