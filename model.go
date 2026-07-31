package bubble_ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// nextID hands out a unique id to every Model created by New, so that
// internal messages from multiple simultaneous instances never cross wires.
var nextID uint64

// Model is a Bubble Tea component that renders a live, interactive SSH
// session. Build one with New, run its Init() Cmd (directly, or batched
// into a parent's Init), and route Update/View like any other Bubble Tea
// model or embeddable sub-model.
//
// The zero value is not usable — always construct via New.
type Model struct {
	// id uniquely identifies this instance so internal async messages never
	// get mixed up with another Model's, if you're running more than one.
	id uint64

	// --- connection config, set by New/Option and immutable afterwards ---
	addr            string
	user            string
	port            int
	authMethods     []ssh.AuthMethod
	hostKeyCallback ssh.HostKeyCallback
	term            string
	env             map[string]string
	width, height   int
	mouseForwarding bool
	connectTimeout  time.Duration
	setupErr        error // first error raised by an Option, surfaced at Init()

	// --- runtime state ---
	state state
	err   error

	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	vt      *vt.Emulator

	outCh  chan tea.Msg
	cancel context.CancelFunc
}

// New creates a Model for the given address ("host" or "host:port"). Call
// options like WithUser and WithAgent/WithPassword/WithPrivateKey to
// configure authentication before use — nothing is connected yet, that
// happens when Init()'s command runs.
func New(addr string, opts ...Option) Model {
	m := Model{
		id:             atomic.AddUint64(&nextID, 1),
		addr:           addr,
		port:           22,
		term:           "xterm-256color",
		width:          80,
		height:         24,
		connectTimeout: 10 * time.Second,
		state:          stateConnecting,
	}
	for _, opt := range opts {
		opt(&m)
	}
	if m.setupErr != nil {
		m.state = stateError
		m.err = m.setupErr
	}
	return m
}

// Init satisfies tea.Model. It kicks off the SSH connection asynchronously;
// nothing blocks here.
func (m Model) Init() tea.Cmd {
	if m.setupErr != nil {
		id := m.id
		err := m.setupErr
		return func() tea.Msg { return errMsg{id: id, err: err} }
	}
	return m.connect
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case connectedMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.state = stateConnected
		m.client = msg.client
		m.session = msg.session
		m.stdin = msg.stdin
		m.vt = msg.term
		m.outCh = msg.outCh
		m.cancel = msg.cancel
		return m, waitForActivity(m.id, m.outCh)

	case outputMsg:
		if msg.id != m.id {
			return m, nil
		}
		if m.vt != nil {
			_, _ = m.vt.Write(msg.data)
		}
		return m, waitForActivity(m.id, m.outCh)

	case closedMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.state = stateClosed
		m.err = msg.err
		return m, nil

	case errMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.state = stateError
		m.err = msg.err
		return m, nil

	case tea.KeyPressMsg:
		if m.state == stateConnected {
			m.sendKey(msg)
		}
		return m, nil

	case tea.MouseMsg:
		if m.mouseForwarding && m.state == stateConnected {
			m.sendMouse(msg)
		}
		return m, nil
	}

	return m, nil
}

// View satisfies tea.Model.
func (m Model) View() tea.View {
	return tea.NewView(m.Content())
}

// Content returns the current screen content as a plain styled string
// (ANSI colors/links included, no cursor-positioning wrapper). Use this
// instead of View() when embedding the pane inside a larger layout, e.g.
// with lipgloss.JoinHorizontal.
func (m Model) Content() string {
	switch m.state {
	case stateConnecting:
		return fmt.Sprintf("connecting to %s…", m.dialAddr())
	case stateError:
		return fmt.Sprintf("ssh error: %v", m.err)
	case stateClosed:
		if m.err != nil && m.err != io.EOF {
			return fmt.Sprintf("connection closed: %v", m.err)
		}
		return "connection closed"
	default:
		if m.vt == nil {
			return ""
		}
		return m.vt.Render()
	}
}

// Connected reports whether the SSH session is currently up.
func (m Model) Connected() bool { return m.state == stateConnected }

// Err returns the last error (connection failure or unexpected close), if
// any.
func (m Model) Err() error { return m.err }

// SetSize resizes the PTY, both locally (the virtual terminal emulator) and
// on the remote end (an SSH "window-change" request). Wire this to
// tea.WindowSizeMsg yourself if this pane should track the full window —
// it's not done automatically since an embedded pane is often smaller than
// the whole screen.
func (m Model) SetSize(cols, rows int) (Model, tea.Cmd) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	m.width, m.height = cols, rows
	if m.vt != nil {
		m.vt.Resize(cols, rows)
	}
	if m.session != nil {
		sess := m.session
		return m, func() tea.Msg {
			_ = sess.WindowChange(rows, cols)
			return nil
		}
	}
	return m, nil
}

// Close tears down the SSH session and the underlying TCP connection. Call
// it when you're done with this pane — Bubble Tea has no "unmount" hook, so
// nothing does this for you automatically (e.g. call it before switching
// away from this pane, or on program exit).
func (m Model) Close() error {
	if m.cancel != nil {
		m.cancel()
	}
	if m.vt != nil {
		_ = m.vt.Close()
	}
	var err error
	if m.session != nil {
		err = m.session.Close()
	}
	if m.client != nil {
		if cerr := m.client.Close(); err == nil {
			err = cerr
		}
	}
	if err == io.EOF {
		return nil
	}
	return err
}

func (m Model) dialAddr() string {
	if _, _, err := net.SplitHostPort(m.addr); err == nil {
		return m.addr
	}
	return net.JoinHostPort(m.addr, fmt.Sprintf("%d", m.port))
}

// defaultKnownHosts returns the default ~/.ssh/known_hosts path, resolved.
func defaultKnownHosts() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// resolveHostKeyCallback returns the configured callback, or falls back to
// the default known_hosts file. It deliberately does NOT fall back to
// InsecureIgnoreHostKey — that must be opted into explicitly.
func (m Model) resolveHostKeyCallback() (ssh.HostKeyCallback, error) {
	if m.hostKeyCallback != nil {
		return m.hostKeyCallback, nil
	}
	path, err := defaultKnownHosts()
	if err != nil {
		return nil, fmt.Errorf("bubble_ssh: no host key verification configured and couldn't resolve a default known_hosts (%w) — call WithKnownHostsFile or WithInsecureIgnoreHostKey explicitly", err)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("bubble_ssh: no host key verification configured and %s doesn't exist — call WithKnownHostsFile or WithInsecureIgnoreHostKey explicitly", path)
	}
	return knownhosts.New(path)
}
