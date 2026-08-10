// Command basic is a minimal full-screen SSH client built around
// bubblessh.Model: the root model owns quitting and window resizes, then
// delegates everything else to the SSH pane.
//
//	go run ./basic -addr bandit.labs.overthewire.org:2220 -user bandit0 -accept-new-host-keys ~/.ssh/known_hosts
//
// Password auth prompts with echo disabled. Set SSH_PASSWORD to skip the prompt.
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

const (
	keyQuit = "ctrl+q"

	defaultAddr = "localhost:22"
	defaultCols = 80
	defaultRows = 24

	authPassword  = "password"
	authPublicKey = "publickey"
	defaultKey    = "~/.ssh/id_ed25519"
)

type appModel struct {
	ssh bubblessh.Model
}

func (a appModel) Init() tea.Cmd {
	return a.ssh.Init()
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == keyQuit {
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
		return a, tea.Sequence(cmd, tea.Quit)
	}
	return a, cmd
}

func (a appModel) View() tea.View {
	return a.ssh.View()
}

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

func readPassword() (string, error) {
	if pw := os.Getenv("SSH_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Print("Enter password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(b), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bubblessh:", err)
	os.Exit(1)
}

func main() {
	addr := flag.String("addr", defaultAddr, "host:port to connect to")
	user := flag.String("user", os.Getenv("USER"), "SSH username")
	method := flag.String("method", authPassword, "authentication method: password or publickey")
	privateKey := flag.String("i", defaultKey, "private key file (publickey only)")
	privateKeyPassphrase := flag.String("k", "", "private key passphrase (publickey only)")
	acceptNewHostKeys := flag.String("accept-new-host-keys", "", "known_hosts file for accept-new host key policy")
	insecureIgnoreHostKey := flag.Bool("insecure-ignore-host-key", false, "skip host key verification (testing only)")
	flag.Parse()

	opts := []bubblessh.Option{
		bubblessh.WithUser(*user),
		bubblessh.WithSize(defaultCols, defaultRows),
	}

	switch *method {
	case authPassword:
		password, err := readPassword()
		if err != nil {
			fatal(err)
		}
		opts = append(opts, bubblessh.WithPassword(password))
	case authPublicKey:
		opts = append(opts, bubblessh.WithPrivateKeyFile(expandHome(*privateKey), *privateKeyPassphrase))
	default:
		fmt.Fprintf(os.Stderr, "bubblessh: invalid -method %q (want %s or %s)\n", *method, authPassword, authPublicKey)
		os.Exit(1)
	}

	switch {
	case *insecureIgnoreHostKey:
		opts = append(opts, bubblessh.WithInsecureIgnoreHostKey())
	case *acceptNewHostKeys != "":
		opts = append(opts, bubblessh.WithAcceptNewHostKeys(expandHome(*acceptNewHostKeys)))
	}

	final, err := tea.NewProgram(appModel{ssh: bubblessh.New(*addr, opts...)}).Run()
	if err != nil {
		fatal(err)
	}
	if a, ok := final.(appModel); ok {
		if sshErr := a.ssh.Err(); sshErr != nil {
			fatal(sshErr)
		}
	}
}
