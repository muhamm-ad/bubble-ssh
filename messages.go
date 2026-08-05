package bubblessh

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/crypto/ssh"
)

// state is the internal connection lifecycle of a Model.
type state int

const (
	stateIdle state = iota
	stateConnecting
	stateConnected
	stateClosed
	stateError
)

// Every internal message carries the id of the Model instance it belongs to.
// This is what makes it safe to run several bubblessh.Model instances in
// the same Bubbletea program (e.g. a split-pane multi-server view): a parent
// can forward any message to every child's Update() unconditionally
// — each Model silently ignores messages that aren't its own instead of
// requiring the caller to route them correctly by hand.

// connectedMsg is delivered once the SSH connection, PTY, and shell are all up.
// It carries the live handles that Update() will store on the Model.
type connectedMsg struct {
	id uint64

	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	term    *vt.Emulator

	outCh  chan tea.Msg
	cancel context.CancelFunc
}

// outputMsg is a chunk of raw bytes read from the remote shell's stdout.
// It is written into the vt.Emulator from inside Update(), never from the
// reader goroutine, so there's no data race on the emulator's state.
type outputMsg struct {
	id   uint64
	data []byte
}

// closedMsg means the remote side (or the connection) went away.
type closedMsg struct {
	id  uint64
	err error
}

// errMsg is a fatal error that happened while dialing, authenticating, or
// setting up the session.
type errMsg struct {
	id  uint64
	err error
}
