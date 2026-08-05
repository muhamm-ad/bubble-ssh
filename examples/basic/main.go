// Command basic is a minimal, full-screen SSH client built around
// bubblessh.Model. It shows the recommended way to use the component: wrap it
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
	bubblessh "github.com/muhamm-ad/bubble-ssh"
)

// appModel wraps bubblessh.Model so the root program controls when to quit —
// bubblessh.Model itself never calls tea.Quit, since it's meant to be safely
// embeddable inside bigger programs too.
type appModel struct {
	ssh bubblessh.Model
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
	a.ssh = m.(bubblessh.Model)

	if !a.ssh.Connected() && a.ssh.Err() != nil {
		// Either the initial connection failed or the remote end hung up.
		// Let the current command (if any) run, then quit.
		fmt.Fprintln(os.Stderr, "bubblessh:", a.ssh.Err())
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

	opts := []bubblessh.Option{
		bubblessh.WithUser(*user),
		bubblessh.WithSize(80, 24),
	}
	if *password != "" {
		opts = append(opts, bubblessh.WithPassword(*password))
	} else {
		opts = append(opts, bubblessh.WithAgent())
	}
	if *insecure {
		opts = append(opts, bubblessh.WithInsecureIgnoreHostKey())
	}

	m := appModel{ssh: bubblessh.New(*addr, opts...)}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bubblessh:", err)
		os.Exit(1)
	}
}
