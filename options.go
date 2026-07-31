package bubble_ssh

import (
	"fmt"
	"net"
	"os"
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
			m.setupErr = fmt.Errorf("bubble_ssh: parsing private key: %w", err)
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
			m.setupErr = fmt.Errorf("bubble_ssh: reading private key %s: %w", path, err)
			return
		}
		signer, err := parsePEM(pemBytes, passphrase)
		if err != nil {
			m.setupErr = fmt.Errorf("bubble_ssh: parsing private key %s: %w", path, err)
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

// WithAgent adds authentication via a running ssh-agent, using the
// SSH_AUTH_SOCK environment variable. It's resolved lazily at connection
// time, so it's safe to call even if no agent is running yet (it'll just
// fail at Connect time with a clear error).
func WithAgent() Option {
	return func(m *Model) {
		m.authMethods = append(m.authMethods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			sock := os.Getenv("SSH_AUTH_SOCK")
			if sock == "" {
				return nil, fmt.Errorf("bubble_ssh: SSH_AUTH_SOCK is not set, is ssh-agent running?")
			}
			conn, err := net.Dial("unix", sock)
			if err != nil {
				return nil, fmt.Errorf("bubble_ssh: connecting to ssh-agent: %w", err)
			}
			return agent.NewClient(conn).Signers()
		}))
	}
}

// WithPort overrides the port. If you already included a port in the addr
// passed to New (e.g. "host:2222"), this is not needed.
func WithPort(port int) Option {
	return func(m *Model) { m.port = port }
}

// WithKnownHostsFile verifies the server's host key against one or more
// OpenSSH-format known_hosts files (e.g. "~/.ssh/known_hosts", expanded by
// you). If you never call this or WithInsecureIgnoreHostKey, Connect will
// try the default "~/.ssh/known_hosts" and fail loudly if it can't find it,
// rather than silently skipping verification.
func WithKnownHostsFile(paths ...string) Option {
	return func(m *Model) {
		cb, err := knownhosts.New(paths...)
		if err != nil {
			m.setupErr = fmt.Errorf("bubble_ssh: loading known_hosts: %w", err)
			return
		}
		m.hostKeyCallback = cb
	}
}

// WithInsecureIgnoreHostKey disables host key verification entirely. This
// makes the connection vulnerable to man-in-the-middle attacks — only use
// it for throwaway boxes (e.g. a container on localhost) or local testing.
func WithInsecureIgnoreHostKey() Option {
	return func(m *Model) { m.hostKeyCallback = ssh.InsecureIgnoreHostKey() } //nolint:gosec
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
// directive — this is a server-side restriction bubble_ssh can't work around.
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
