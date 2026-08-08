package bubblessh

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/crypto/ssh"
)

// connect dials, authenticates, opens a session, requests a PTY, and starts a shell.
// It's a plain tea.Cmd (Bubble Tea already runs Cmds on their own
// goroutine), so it can block freely.
func (m Model) connect() tea.Msg {
	hostKeyCallback, err := m.resolveHostKeyCallback()
	if err != nil {
		return errMsg{id: m.id, err: err}
	}

	cfg := &ssh.ClientConfig{
		User:            m.user,
		Auth:            m.authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         m.connectTimeout,
	}

	client, err := ssh.Dial("tcp", m.dialAddr(), cfg)
	if err != nil {
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: dial %s: %w", m.dialAddr(), err)}
	}

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: new session: %w", err)}
	}

	for k, v := range m.env {
		// Best-effort: most sshd configs only forward variables listed in
		// their AcceptEnv directive, so this can silently be a no-op
		// server-side. That's an OpenSSH restriction, not a bug here.
		_ = session.Setenv(k, v)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(m.term, m.height, m.width, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: request pty: %w", err)}
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: stdin pipe: %w", err)}
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: stdout pipe: %w", err)}
	}
	// With a PTY allocated, the remote shell's stderr is written to the
	// same pty device as stdout, so there is no separate stream to read here —
	// this mirrors what a normal interactive `ssh` session does.

	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return errMsg{id: m.id, err: fmt.Errorf("bubblessh: start shell: %w", err)}
	}

	term := vt.NewEmulator(m.width, m.height)

	// Tracks DECTCEM (cursor show/hide, `\x1b[?25l`/`\x1b[?25h`) so View() can honor it
	cursorVisible := true
	term.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { cursorVisible = visible },
	})

	outCh := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())

	go readLoop(ctx, m.id, stdout, outCh)
	go func() {
		// Forward anything SendKey()/SendText()/Paste() encodes on the
		// emulator's input pipe straight to the remote shell's stdin.
		_, _ = io.Copy(stdin, term)
	}()

	return connectedMsg{
		id:            m.id,
		client:        client,
		session:       session,
		stdin:         stdin,
		term:          term,
		cursorVisible: &cursorVisible,
		outCh:         outCh,
		cancel:        cancel,
	}
}

// readLoop copies remote output onto outCh in chunks. It never touches the
// vt.Emulator directly — that only ever happens inside Update(), on Bubble
// Tea's single event-loop goroutine.
func readLoop(ctx context.Context, id uint64, r io.Reader, outCh chan<- tea.Msg) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case outCh <- outputMsg{id: id, data: chunk}:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			select {
			case outCh <- closedMsg{id: id, err: err}:
			case <-ctx.Done():
			}
			return
		}
	}
}

// waitForActivity is the standard Bubble Tea "listen on a channel" command:
// it blocks for exactly one message, then Update() re-issues it to keep
// listening. See the official "realtime" example for the same pattern.
func waitForActivity(id uint64, ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return closedMsg{id: id, err: io.EOF}
		}
		return msg
	}
}
