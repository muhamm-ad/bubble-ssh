// Command split-pane connects to two hosts at once and shows them side by
// side, Tab to switch which pane receives keystrokes. It demonstrates that
// bubblessh.Model instances are safe to run concurrently in the same program —
// each one tags its internal messages with its own id, so a parent can
// broadcast any non-key message to every child without routing it by hand.
//
//	go run ./split-pane -left alice@host1:22 -right bob@host2:22
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/muhamm-ad/bubble-ssh"
)

type appModel struct {
	left, right   bubblessh.Model
	leftAddr      string
	rightAddr     string
	focus         int // 0 = left, 1 = right
	width, height int
}

func (a appModel) Init() tea.Cmd {
	return tea.Batch(a.left.Init(), a.right.Init())
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			_ = a.left.Close()
			_ = a.right.Close()
			return a, tea.Quit
		case "tab":
			a.focus = 1 - a.focus
			return a, nil
		}
		// Any other key goes only to the focused pane.
		var cmd tea.Cmd
		var m tea.Model
		if a.focus == 0 {
			m, cmd = a.left.Update(msg)
			a.left = m.(bubblessh.Model)
		} else {
			m, cmd = a.right.Update(msg)
			a.right = m.(bubblessh.Model)
		}
		return a, cmd

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		outerW := a.width / 2
		outerH := a.height - 1                   // reserve one line for the status bar
		contentW, contentH := outerW-2, outerH-2 // -2 each: the pane border
		if contentW < 1 {
			contentW = 1
		}
		if contentH < 1 {
			contentH = 1
		}
		var lcmd, rcmd tea.Cmd
		a.left, lcmd = a.left.SetSize(contentW, contentH)
		a.right, rcmd = a.right.SetSize(contentW, contentH)
		return a, tea.Batch(lcmd, rcmd)
	}

	// Everything else — bubblessh's own connectedMsg/outputMsg/closedMsg/errMsg
	// mainly — gets broadcast to both panes. Each Model silently ignores
	// messages that aren't addressed to it, so this is safe even though
	// only one pane will actually match any given message.
	lm, lcmd := a.left.Update(msg)
	a.left = lm.(bubblessh.Model)
	rm, rcmd := a.right.Update(msg)
	a.right = rm.(bubblessh.Model)
	return a, tea.Batch(lcmd, rcmd)
}

func (a appModel) View() tea.View {
	outerW := a.width / 2
	outerH := a.height - 1
	contentW, contentH := outerW-2, outerH-2
	if contentW < 1 {
		contentW = 1
	}
	if contentH < 1 {
		contentH = 1
	}

	left := paneStyle(a.focus == 0, contentW, contentH).Render(a.left.Content())
	right := paneStyle(a.focus == 1, contentW, contentH).Render(a.right.Content())
	row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	status := fmt.Sprintf("[%s | %s]  tab: switch pane  •  ctrl+c: quit", a.leftAddr, a.rightAddr)
	status = lipgloss.NewStyle().Faint(true).Render(status)

	return tea.NewView(strings.Join([]string{row, status}, "\n"))
}

func paneStyle(focused bool, w, h int) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(w).Height(h)
	if focused {
		return s.BorderForeground(lipgloss.Color("212"))
	}
	return s.BorderForeground(lipgloss.Color("240"))
}

func main() {
	left := flag.String("left", "", "left pane, as user@host:port")
	right := flag.String("right", "", "right pane, as user@host:port")
	insecure := flag.Bool("insecure-ignore-host-key", false, "skip host key verification (testing only!)")
	flag.Parse()

	if *left == "" || *right == "" {
		fmt.Fprintln(os.Stderr, "usage: split-pane -left user@host:port -right user@host:port")
		os.Exit(2)
	}

	leftUser, leftAddr := splitUserHost(*left)
	rightUser, rightAddr := splitUserHost(*right)

	leftOpts := []bubblessh.Option{bubblessh.WithUser(leftUser), bubblessh.WithAgent()}
	rightOpts := []bubblessh.Option{bubblessh.WithUser(rightUser), bubblessh.WithAgent()}
	if *insecure {
		leftOpts = append(leftOpts, bubblessh.WithInsecureIgnoreHostKey())
		rightOpts = append(rightOpts, bubblessh.WithInsecureIgnoreHostKey())
	}

	m := appModel{
		left:      bubblessh.New(leftAddr, leftOpts...),
		right:     bubblessh.New(rightAddr, rightOpts...),
		leftAddr:  *left,
		rightAddr: *right,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bubblessh:", err)
		os.Exit(1)
	}
}

// splitUserHost splits "user@host:port" into ("user", "host:port").
func splitUserHost(s string) (user, addr string) {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return os.Getenv("USER"), s
}
