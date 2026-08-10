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

func (p *pane) updateAwaitingPassword(msg tea.KeyPressMsg, cols, rows int) tea.Cmd {
	switch msg.String() {
	case "enter":
		return p.start(cols, rows)
	case "backspace":
		if len(p.password) > 0 {
			p.password = p.password[:len(p.password)-1]
		}
	default:
		if len(msg.Text) > 0 {
			p.password += msg.Text
		}
	}
	return nil
}

func (p *pane) content(cols, rows int) string {
	var raw string
	if p.started {
		raw = p.ssh.Content()
	} else {
		masked := strings.Repeat("*", len(p.password))
		raw = fmt.Sprintf("%s@%s\n\npassword: %s\n\n(enter to connect)", p.user, p.addr, masked)
	}
	return fitPane(raw, cols, rows)
}

func fitPane(s string, cols, rows int) string {
	if rows < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	} else {
		for len(lines) < rows {
			lines = append(lines, "")
		}
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if cols > 0 && lipgloss.Width(line) > cols {
			out[i] = lipgloss.NewStyle().MaxWidth(cols).Render(line)
		} else {
			out[i] = line
		}
	}
	return strings.Join(out, "\n")
}

type appModel struct {
	left, right   pane
	focus         int
	width, height int
}

func (a appModel) Init() tea.Cmd {
	return nil
}

func (a appModel) paneLayout() (cols, rows, boxW, boxH int) {
	const (
		statusH = 1
		border  = 2
	)
	boxW = max(1, a.width/2)
	boxH = max(1, a.height-statusH)
	cols = max(1, boxW-border)
	rows = max(1, boxH-border)
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
		case "ctrl+q":
			if a.left.started {
				_ = a.left.ssh.Close()
			}
			if a.right.started {
				_ = a.right.ssh.Close()
			}
			return a, tea.Quit
		case "ctrl+left":
			a.focus = 0
			return a, nil
		case "ctrl+right":
			a.focus = 1
			return a, nil
		}

		cols, rows := a.paneSize()
		if a.focus == 0 {
			if !a.left.started {
				return a, a.left.updateAwaitingPassword(msg, cols, rows)
			}
			m, cmd := a.left.ssh.Update(msg)
			a.left.ssh = m.(bubblessh.Model)
			return a, cmd
		} else {
			if !a.right.started {
				return a, a.right.updateAwaitingPassword(msg, cols, rows)
			}
			m, cmd := a.right.ssh.Update(msg)
			a.right.ssh = m.(bubblessh.Model)
			return a, cmd
		}

	case tea.PasteMsg:
		if a.focus == 0 {
			if !a.left.started {
				a.left.password += msg.Content
				return a, nil
			}
			m, cmd := a.left.ssh.Update(msg)
			a.left.ssh = m.(bubblessh.Model)
			return a, cmd
		} else {
			if !a.right.started {
				a.right.password += msg.Content
				return a, nil
			}
			m, cmd := a.right.ssh.Update(msg)
			a.right.ssh = m.(bubblessh.Model)
			return a, cmd
		}

	case tea.MouseMsg:
		var cmd tea.Cmd

		switch a.focus {
		case 0:
			if !a.left.started {
				return a, nil
			}
			m, c := a.left.ssh.Update(msg)
			a.left.ssh = m.(bubblessh.Model)
			cmd = c
		case 1:
			if !a.right.started {
				return a, nil
			}
			m, c := a.right.ssh.Update(msg)
			a.right.ssh = m.(bubblessh.Model)
			cmd = c
		}
		return a, cmd

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		cols, rows := a.paneSize()
		var lcmd, rcmd tea.Cmd
		if a.left.started {
			a.left.ssh, lcmd = a.left.ssh.SetSize(cols, rows)
		}
		if a.right.started {
			a.right.ssh, rcmd = a.right.ssh.SetSize(cols, rows)
		}
		return a, tea.Batch(lcmd, rcmd)
	}

	var lcmd, rcmd tea.Cmd
	if a.left.started {
		lm, cmd := a.left.ssh.Update(msg)
		a.left.ssh = lm.(bubblessh.Model)
		lcmd = cmd
	}
	if a.right.started {
		rm, cmd := a.right.ssh.Update(msg)
		a.right.ssh = rm.(bubblessh.Model)
		rcmd = cmd
	}
	return a, tea.Batch(lcmd, rcmd)
}

func (a appModel) View() tea.View {
	cols, rows, boxW, boxH := a.paneLayout()

	left := paneStyle(a.focus == 0, boxW, boxH).Render(a.left.content(cols, rows))
	right := paneStyle(a.focus == 1, boxW, boxH).Render(a.right.content(cols, rows))
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	status := lipgloss.NewStyle().Faint(true).Render("ctrl+<-: focus left  •  ctrl+->: focus right  •  enter: connect  •  ctrl+q: quit")

	result := lipgloss.JoinVertical(lipgloss.Left, panels, status)
	view := tea.NewView(result)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	focused := &a.left
	xOffset := 1
	if a.focus == 1 {
		focused = &a.right
		xOffset = a.width/2 + 1
	}
	if focused.started {
		if cur := focused.ssh.Cursor(); cur != nil {
			cur.X += xOffset
			cur.Y += 1
			view.Cursor = cur
		}
	}

	return view
}

func paneStyle(focused bool, boxW, boxH int) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(boxW).Height(boxH)
	if focused {
		return s.BorderForeground(lipgloss.Color("212"))
	}
	return s.BorderForeground(lipgloss.Color("240"))
}

func main() {
	left := flag.String("left", "", "left pane, as user@host:port")
	right := flag.String("right", "", "right pane, as user@host:port")
	flag.Parse()

	if *left == "" || *right == "" {
		fmt.Fprintln(os.Stderr, "usage: split-pane -left user@host:port -right user@host:port")
		os.Exit(2)
	}

	leftUser, leftAddr, leftPort := splitUserHost(*left)
	rightUser, rightAddr, rightPort := splitUserHost(*right)

	m := appModel{
		left:   pane{addr: leftAddr, user: leftUser, port: leftPort},
		right:  pane{addr: rightAddr, user: rightUser, port: rightPort},
		width:  80,
		height: 24,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bubblessh:", err)
		os.Exit(1)
	}
}

func splitUserHost(s string) (user, addr string, port int) {
	if i := strings.Index(s, "@"); i >= 0 {
		user = s[:i]
		addr = s[i+1:]
		if j := strings.Index(addr, ":"); j >= 0 {
			port, _ = strconv.Atoi(addr[j+1:])
			addr = addr[:j]
		}
		return user, addr, port
	}
	return os.Getenv("USER"), "", 22
}
