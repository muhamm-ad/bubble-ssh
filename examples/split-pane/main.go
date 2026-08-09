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

func (p *pane) content() string {
	if p.started {
		return p.ssh.Content()
	}
	masked := strings.Repeat("*", len(p.password))
	return fmt.Sprintf("%s@%s\n\npassword: %s\n\n(enter to connect)", p.user, p.addr, masked)
}

type appModel struct {
	left, right   pane
	focus         int
	width, height int
}

func (a appModel) Init() tea.Cmd {
	return nil
}

func (a appModel) paneSize() (cols, rows int) {
	outerW := a.width / 2
	cols, rows = outerW-2, a.height-2
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
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
	cols, rows := a.paneSize()

	left := paneStyle(a.focus == 0, cols, rows).Render(a.left.content())
	right := paneStyle(a.focus == 1, cols, rows).Render(a.right.content())
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
		xOffset = a.width / 2
	}
	if focused.started {
		if cur := focused.ssh.Cursor(); cur != nil {
			cur.X += xOffset
			cur.Y++
			view.Cursor = cur
		}
	}

	return view
}

func paneStyle(focused bool, width, height int) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width).Height(height)
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
