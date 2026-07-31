// Package bubble_ssh embeds a real, interactive SSH session inside a Bubble Tea
// (charm.land/bubbletea/v2) model.
//
// It opens an SSH connection with golang.org/x/crypto/ssh, requests a PTY,
// starts a remote shell, and feeds the remote output through a virtual
// terminal emulator (github.com/charmbracelet/x/vt) that understands ANSI/
// VT220 escape sequences. The emulator's rendered screen becomes the
// Model's View(), and keystrokes typed into the Bubble Tea program are
// encoded and forwarded to the remote shell — the same way a normal `ssh`
// client would behave, but as a component you can drop into any TUI.
//
// # Architecture
//
//	┌──────────────┐  bytes   ┌──────────────┐  screen   ┌────────────┐
//	│ SSH session  │ ───────► │ vt.Emulator  │ ────────► │  View()    │
//	│ (remote pty) │ ◄─────── │ (ANSI state) │           │ (string)   │
//	└──────────────┘  bytes   └──────────────┘           └────────────┘
//	       ▲                         ▲
//	       │                         │ SendKey()
//	       │                  ┌──────┴───────────┐
//	       └── stdin.Write ── │  tea.KeyPressMsg │
//	                          └──────────────────┘
//
// All calls into the vt.Emulator (Write, Resize, Render) happen on Bubble
// Tea's single Update/View goroutine, so there is no manual locking. Reading
// SSH stdout happens on a background goroutine that only ever pushes bytes
// onto a channel — the actual terminal-state mutation happens inside
// Update(), which is what Bubble Tea guarantees is single-threaded.
//
// # Basic usage
//
//	m := bubble_ssh.New("example.com:22",
//		bubble_ssh.WithUser("alice"),
//		bubble_ssh.WithAgent(),
//		bubble_ssh.WithSize(80, 24),
//	)
//	p := tea.NewProgram(m)
//	if _, err := p.Run(); err != nil {
//		log.Fatal(err)
//	}
//
// See the examples/ directory for a standalone full-screen client and for
// embedding the pane as one half of a split-screen layout.
package bubble_ssh
