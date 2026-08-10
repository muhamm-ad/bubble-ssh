// Command split-pane connects to two hosts at once and shows them side by
// side. Each pane asks for its password inline before connecting.
//
//	go run ./split-pane -left bandit0@bandit.labs.overthewire.org:2220 -right bandit1@bandit.labs.overthewire.org:2220
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	bubblessh "github.com/muhamm-ad/bubble-ssh"
)

const (
	focusLeft  = 0
	focusRight = 1

	keyQuit       = "ctrl+q"
	keyFocusLeft  = "ctrl+left"
	keyFocusRight = "ctrl+right"

	defaultCols = 80
	defaultRows = 24
	defaultPort = 22

	// Layout chrome. lipgloss v2 Width/Height include the border, so the PTY
	// (inner) size is box size minus borderSize on each axis.
	statusBarRows = 1
	borderSize    = 2
	paneCount     = 2

	focusColor   = "212"
	unfocusColor = "240"

	statusText = "ctrl+<-: focus left  •  ctrl+->: focus right  •  enter: connect  •  ctrl+q: quit"
)

type pane struct {
	addr, user string
	port       int
	started    bool
	password   string
	ssh        bubblessh.Model
}

func (p *pane) start(cols, rows int) tea.Cmd {
	p.ssh = bubblessh.New(p.addr,
		bubblessh.WithPort(p.port),
		bubblessh.WithUser(p.user),
		bubblessh.WithPassword(p.password),
		bubblessh.WithSize(cols, rows),
	)
	p.started = true
	return p.ssh.Init()
}

func (p *pane) updatePassword(msg tea.KeyPressMsg, cols, rows int) tea.Cmd {
	switch msg.String() {
	case "enter":
		return p.start(cols, rows)
	case "backspace":
		if len(p.password) > 0 {
			p.password = p.password[:len(p.password)-1]
		}
	default:
		p.password += msg.Text
	}
	return nil
}

func (p *pane) content(cols, rows int) string {
	if p.started {
		return p.ssh.Content()
	}
	masked := strings.Repeat("*", len(p.password))
	prompt := fmt.Sprintf("%s@%s\n\npassword: %s\n\n(enter to connect)", p.user, p.addr, masked)
	return fitBlock(prompt, cols, rows)
}

func fitBlock(s string, cols, rows int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	for i, line := range lines {
		if lipgloss.Width(line) > cols {
			lines[i] = lipgloss.NewStyle().MaxWidth(cols).Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

type appModel struct {
	left, right   pane
	focus         int
	width, height int
}

func (a appModel) Init() tea.Cmd { return nil }

func (a *appModel) focused() *pane {
	if a.focus == focusRight {
		return &a.right
	}
	return &a.left
}

func (a appModel) paneLayout() (cols, rows, boxW, boxH int) {
	boxW = max(1, a.width/paneCount)
	boxH = max(1, a.height-statusBarRows)
	cols = max(1, boxW-borderSize)
	rows = max(1, boxH-borderSize)
	return cols, rows, boxW, boxH
}

func (a appModel) paneSize() (cols, rows int) {
	cols, rows, _, _ = a.paneLayout()
	return cols, rows
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		switch msg.String() {
		case keyQuit:
			a.closeAll()
			return a, tea.Quit
		case keyFocusLeft:
			a.focus = focusLeft
			return a, nil
		case keyFocusRight:
			a.focus = focusRight
			return a, nil
		}

		cols, rows := a.paneSize()
		p := a.focused()
		if !p.started {
			return a, p.updatePassword(msg, cols, rows)
		}
		m, cmd := p.ssh.Update(msg)
		p.ssh = m.(bubblessh.Model)
		return a, cmd

	case tea.PasteMsg:
		p := a.focused()
		if !p.started {
			p.password += msg.Content
			return a, nil
		}
		m, cmd := p.ssh.Update(msg)
		p.ssh = m.(bubblessh.Model)
		return a, cmd

	case tea.MouseMsg:
		p := a.focused()
		if !p.started {
			return a, nil
		}
		m, cmd := p.ssh.Update(msg)
		p.ssh = m.(bubblessh.Model)
		return a, cmd

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, a.resizePanes()
	}

	return a, a.forward(msg)
}

func (a *appModel) closeAll() {
	if a.left.started {
		_ = a.left.ssh.Close()
	}
	if a.right.started {
		_ = a.right.ssh.Close()
	}
}

func (a *appModel) resizePanes() tea.Cmd {
	cols, rows := a.paneSize()
	var cmds []tea.Cmd
	if a.left.started {
		var cmd tea.Cmd
		a.left.ssh, cmd = a.left.ssh.SetSize(cols, rows)
		cmds = append(cmds, cmd)
	}
	if a.right.started {
		var cmd tea.Cmd
		a.right.ssh, cmd = a.right.ssh.SetSize(cols, rows)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (a *appModel) forward(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if a.left.started {
		m, cmd := a.left.ssh.Update(msg)
		a.left.ssh = m.(bubblessh.Model)
		cmds = append(cmds, cmd)
	}
	if a.right.started {
		m, cmd := a.right.ssh.Update(msg)
		a.right.ssh = m.(bubblessh.Model)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (a appModel) View() tea.View {
	cols, rows, boxW, boxH := a.paneLayout()

	left := paneStyle(a.focus == focusLeft, boxW, boxH).Render(a.left.content(cols, rows))
	right := paneStyle(a.focus == focusRight, boxW, boxH).Render(a.right.content(cols, rows))
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	status := lipgloss.NewStyle().Faint(true).Render(statusText)

	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, panels, status))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	p := a.focused()
	if p.started {
		if cur := p.ssh.Cursor(); cur != nil {
			cur.X += borderSize / 2
			if a.focus == focusRight {
				cur.X += a.width / paneCount
			}
			cur.Y += borderSize / 2
			view.Cursor = cur
		}
	}

	return view
}

func paneStyle(focused bool, boxW, boxH int) lipgloss.Style {
	color := unfocusColor
	if focused {
		color = focusColor
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(color)).
		Width(boxW).
		Height(boxH)
}

func main() {
	left := flag.String("left", "", "left pane as user@host:port")
	right := flag.String("right", "", "right pane as user@host:port")
	flag.Parse()

	if *left == "" || *right == "" {
		fmt.Fprintln(os.Stderr, "usage: split-pane -left user@host:port -right user@host:port")
		os.Exit(2)
	}

	leftUser, leftAddr, leftPort := parseTarget(*left)
	rightUser, rightAddr, rightPort := parseTarget(*right)

	m := appModel{
		left:   pane{addr: leftAddr, user: leftUser, port: leftPort},
		right:  pane{addr: rightAddr, user: rightUser, port: rightPort},
		width:  defaultCols,
		height: defaultRows,
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bubblessh:", err)
		os.Exit(1)
	}
}

func parseTarget(s string) (user, addr string, port int) {
	port = defaultPort
	user = os.Getenv("USER")

	if i := strings.Index(s, "@"); i >= 0 {
		user = s[:i]
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, ":"); j >= 0 {
		if p, err := strconv.Atoi(s[j+1:]); err == nil {
			port = p
			s = s[:j]
		}
	}
	return user, s, port
}
