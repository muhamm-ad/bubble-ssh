package bubblessh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	cursorShape     CursorShape
	scrollOffset    int
	setupErr        error // first error raised by an Option, surfaced at Init()

	// --- runtime state ---
	state State
	err   error

	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	vt      *vt.Emulator
	// cursorVisible mirrors the vt.Callbacks.CursorVisibility state set up
	// in connect() — see connectedMsg for why it's a pointer.
	cursorVisible *bool

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
		cursorShape:    CursorBar,
		state:          StateConnecting,
	}
	for _, opt := range opts {
		opt(&m)
	}
	if m.setupErr != nil {
		m.state = StateError
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
		m.state = StateConnected
		m.client = msg.client
		m.session = msg.session
		m.stdin = msg.stdin
		m.vt = msg.term
		m.cursorVisible = msg.cursorVisible
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
		m.state = StateClosed
		m.err = msg.err
		return m, nil

	case errMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.state = StateError
		m.err = msg.err
		return m, nil

	case tea.KeyPressMsg:
		if m.state == StateConnected {
			m.scrollOffset = 0
			m.sendKey(msg)
		}
		return m, nil

	case tea.PasteMsg:
		if m.state == StateConnected && m.vt != nil {
			m.scrollOffset = 0
			m.vt.Paste(msg.Content)
		}
		return m, nil

	case tea.MouseMsg:
		if m.state != StateConnected {
			return m, nil
		}
		if m.mouseForwarding {
			m.sendMouse(msg)
			return m, nil
		}
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			m = m.scrollFromWheel(wheel)
		}
		return m, nil
	}

	return m, nil
}

// teaShape converts our own CursorShape (the type WithCursorShape takes) to
// tea.CursorShape (what View() actually needs to hand Bubble Tea). Kept as
// our own type rather than exposing tea.CursorShape directly in the public
// API, same reasoning as every other Option — WithSize takes plain ints,
// not a tea type either.
func (s CursorShape) teaShape() tea.CursorShape {
	switch s {
	case CursorUnderline:
		return tea.CursorUnderline
	case CursorBar:
		return tea.CursorBar
	default:
		return tea.CursorBlock
	}
}

// View satisfies tea.Model.
func (m Model) View() tea.View {
	view := tea.NewView(m.Content())
	view.Cursor = m.Cursor()
	return view
}

// Cursor returns the cursor to draw for the current state, in
// content-local coordinates (0,0 is the top-left of Content()) — or nil if
// no cursor should be shown right now (not connected, or the remote hid
// it). View() uses this directly; if you're composing Content() into a
// bigger layout instead, use this too and offset the position by wherever
// you place that content on screen.
func (m Model) Cursor() *tea.Cursor {
	if m.state != StateConnected || m.vt == nil {
		return nil
	}
	if m.scrollOffset > 0 {
		// Scrolled into history — the live cursor position has nothing to
		// do with what's currently shown, same as a normal terminal hides
		// its cursor while you're scrolled back.
		return nil
	}
	if m.cursorVisible != nil && !*m.cursorVisible {
		return nil
	}
	pos := m.vt.CursorPosition()
	return &tea.Cursor{
		Position: tea.Position{X: pos.X, Y: pos.Y},
		Shape:    m.cursorShape.teaShape(),
		Blink:    true,
	}
}

// Content returns the current screen content as a plain styled string
// (ANSI colors/links included, no cursor-positioning wrapper). Use this
// instead of View() when embedding the pane inside a larger layout, e.g.
// with lipgloss.JoinHorizontal.
//
// The result is always exactly height rows (and at most width cells per
// row), matching the size last set via WithSize/SetSize. Overflow is
// clipped from the top so the bottom — the most recent output — stays
// visible; ScrollUp/ScrollDown already choose which window of history to
// show, and this only clamps that window to the pane.
func (m Model) Content() string {
	var s string
	switch m.state {
	case StateConnecting:
		s = fmt.Sprintf("connecting to %s…", m.dialAddr())
	case StateError:
		s = fmt.Sprintf("ssh error: %v", m.err)
	case StateClosed:
		if m.err != nil && m.err != io.EOF {
			s = fmt.Sprintf("connection closed: %v", m.err)
		} else {
			s = "connection closed"
		}
	default:
		if m.vt == nil {
			s = ""
		} else if m.scrollOffset > 0 {
			s = m.renderScrolled()
		} else {
			s = m.vt.Render()
		}
	}
	return fitBlock(s, m.width, m.height)
}

// Connected reports whether the SSH session is currently up.
func (m Model) Connected() bool { return m.state == StateConnected }

func (m Model) State() State { return m.state }

// Err returns the last error (connection failure or unexpected close), if any.
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
	host := m.addr
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		// A bracketed IPv6 literal with no port, e.g. "[2001:db8::1]".
		// JoinHostPort adds its own brackets around anything containing a
		// colon — without stripping these first we'd end up with
		// "[[2001:db8::1]]:22", which is invalid.
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", m.port))
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
		return nil, fmt.Errorf("bubblessh: no host key verification configured and couldn't resolve a default known_hosts (%w) — call WithKnownHostsFile, WithAcceptNewHostKeys, or WithInsecureIgnoreHostKey explicitly", err)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("bubblessh: no host key verification configured and %s doesn't exist — call WithKnownHostsFile, WithAcceptNewHostKeys, or WithInsecureIgnoreHostKey explicitly", path)
	}
	return knownhosts.New(path)
}
