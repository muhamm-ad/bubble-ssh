// Command basic is a minimal, full-screen SSH client built around
// bubblessh.Model. It shows the recommended way to use the component: wrap it
// in a small root model that owns process-level concerns (quitting,
// forwarding window resizes) and delegates everything else.
//
// go run ./examples/basic -addr bandit.labs.overthewire.org:2220 -user bandit0 -accept-new-host-keys ~/.ssh/known_hosts
// password: bandit0
//
// Password auth always prompts interactively (with echo disabled) — set
// SSH_PASSWORD beforehand to skip the prompt: export SSH_PASSWORD=secret
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	bubblessh "github.com/muhamm-ad/bubble-ssh"
	"golang.org/x/term"
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
		// Don't print here — Bubble Tea still owns the terminal at this
		// point, and Content() already renders this same error on screen.
		// main() reports it again, properly, once the program has exited
		// and the terminal is back to normal.
		return a, tea.Sequence(cmd, tea.Quit)
	}

	return a, cmd
}

func (a appModel) View() tea.View {
	return a.ssh.View()
}

// expandHome replaces a leading "~" with the user's home directory. Go does
// not do this automatically, unlike a shell expanding an unquoted ~ typed
// directly on the command line — a default value baked into the binary, or
// a flag value that reaches us already-quoted, never gets that treatment.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// readPassword prompts for a password without echoing it to the terminal,
// the same way a real ssh client does. Falls back to reading SSH_PASSWORD
// so scripted/CI use doesn't have to sit at an interactive prompt.
func readPassword() (string, error) {
	if pw := os.Getenv("SSH_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Print("Enter password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd())) // int(os.Stdin.Fd()) is the pattern x/term's own doc comment uses
	fmt.Println()                                   // ReadPassword doesn't echo the newline you typed either
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(b), nil
}

func main() {
	addr := flag.String("addr", "localhost:22", "host:port to connect to")
	user := flag.String("user", os.Getenv("USER"), "SSH username")
	method := flag.String("method", "password", "SSH authentication method (password or publickey)")
	privateKey := flag.String("i", "~/.ssh/id_ed25519", "SSH private key file (only used with -method publickey)")
	privateKeyPassphrase := flag.String("k", "", "SSH private key passphrase (only used with -method publickey)")
	acceptNewHostKeys := flag.String("accept-new-host-keys", "", "trust new hosts once and remember them in this known_hosts file (like ssh -o StrictHostKeyChecking=accept-new); leave empty to require an existing ~/.ssh/known_hosts entry")
	insecureIgnoreHostKey := flag.Bool("insecure-ignore-host-key", false, "skip host key verification entirely — testing only, see bubblessh docs")

	flag.Parse()

	opts := []bubblessh.Option{
		bubblessh.WithUser(*user),
		bubblessh.WithSize(80, 24),
	}

	switch *method {
	case "password":
		password, err := readPassword()
		if err != nil {
			fmt.Fprintln(os.Stderr, "bubblessh:", err)
			os.Exit(1)
		}
		opts = append(opts, bubblessh.WithPassword(password))
	case "publickey":
		opts = append(opts, bubblessh.WithPrivateKeyFile(expandHome(*privateKey), *privateKeyPassphrase))
	default:
		fmt.Fprintln(os.Stderr, "bubblessh: invalid -method:", *method, "(want password or publickey)")
		os.Exit(1)
	}

	switch {
	case *insecureIgnoreHostKey:
		opts = append(opts, bubblessh.WithInsecureIgnoreHostKey())
	case *acceptNewHostKeys != "":
		opts = append(opts, bubblessh.WithAcceptNewHostKeys(expandHome(*acceptNewHostKeys)))
	}
	// Neither flag given: bubblessh falls back to a strict check against
	// the default ~/.ssh/known_hosts on its own — nothing to add here.

	m := appModel{ssh: bubblessh.New(*addr, opts...)}

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bubblessh:", err)
		os.Exit(1)
	}
	if a, ok := final.(appModel); ok {
		if sshErr := a.ssh.Err(); sshErr != nil {
			fmt.Fprintln(os.Stderr, "bubblessh:", sshErr)
			os.Exit(1)
		}
	}
}