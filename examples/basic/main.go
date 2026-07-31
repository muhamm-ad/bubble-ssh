// Command basic is a minimal, full-screen SSH client built around
// bubble-ssh.Model. It shows the recommended way to use the component: wrap it
// in a small root model that owns process-level concerns (quitting,
// forwarding window resizes) and delegates everything else.
//
//	go run ./examples/basic -addr example.com:22 -user alice
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/muhamm-ad/bubble-ssh"
)

// appModel wraps bubble-ssh.Model so the root program controls when to quit —
// bubble-ssh.Model itself never calls tea.Quit, since it's meant to be safely
// embeddable inside bigger programs too.
type appModel struct {
	ssh bubble_ssh.Model
}

func (a appModel) Init() tea.Cmd {
	return a.ssh.Init()
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Ctrl+C always quits the local program, before anything is
		// forwarded to the remote shell. If you'd rather Ctrl+C reach the
		// remote shell instead (like a real ssh client), drop this case —
		// closing the remote session (e.g. typing "exit") still quits the
		// program via the disconnect check below.
		if msg.String() == "ctrl+c" {
			_ = a.ssh.Close()
			return a, tea.Quit
		}

	case tea.WindowSizeMsg:
		var cmd tea.Cmd
		a.ssh, cmd = a.ssh.SetSize(msg.Width, msg.Height)
		return a, cmd
	}

	m, cmd := a.ssh.Update(msg)
	a.ssh = m.(bubble_ssh.Model)

	if !a.ssh.Connected() && a.ssh.Err() != nil {
		// Either the initial connection failed or the remote end hung up.
		// Let the current command (if any) run, then quit.
		return a, tea.Sequence(cmd, tea.Quit)
	}

	return a, cmd
}

func (a appModel) View() tea.View {
	return a.ssh.View()
}

func main() {
	addr := flag.String("addr", "localhost:22", "host:port to connect to")
	user := flag.String("user", os.Getenv("USER"), "SSH username")
	password := flag.String("password", "", "SSH password (omit to use ssh-agent instead)")
	insecure := flag.Bool("insecure-ignore-host-key", false, "skip host key verification (testing only!)")
	flag.Parse()

	opts := []bubble_ssh.Option{
		bubble_ssh.WithUser(*user),
		bubble_ssh.WithSize(80, 24),
	}
	if *password != "" {
		opts = append(opts, bubble_ssh.WithPassword(*password))
	} else {
		opts = append(opts, bubble_ssh.WithAgent())
	}
	if *insecure {
		opts = append(opts, bubble_ssh.WithInsecureIgnoreHostKey())
	}

	m := appModel{ssh: bubble_ssh.New(*addr, opts...)}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bubble_ssh:", err)
		os.Exit(1)
	}
}
