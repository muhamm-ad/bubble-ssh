package bubblessh

import "testing"

func TestNewDefaults(t *testing.T) {
	m := New("example.com", WithUser("alice"))

	if m.user != "alice" {
		t.Errorf("user = %q, want alice", m.user)
	}
	if m.port != 22 {
		t.Errorf("port = %d, want 22", m.port)
	}
	if m.width != 80 || m.height != 24 {
		t.Errorf("size = %dx%d, want 80x24", m.width, m.height)
	}
	if m.term != "xterm-256color" {
		t.Errorf("term = %q, want xterm-256color", m.term)
	}
	if m.state != stateConnecting {
		t.Errorf("state = %v, want stateConnecting", m.state)
	}
}

func TestDialAddr(t *testing.T) {
	cases := []struct {
		addr string
		port int
		want string
	}{
		{"example.com", 22, "example.com:22"},
		{"example.com", 2222, "example.com:2222"},
		{"example.com:2200", 22, "example.com:2200"},     // explicit port wins
		{"2001:db8::1", 22, "[2001:db8::1]:22"},          // bare IPv6, no brackets, no port
		{"::1", 22, "[::1]:22"},                          // loopback IPv6
		{"[2001:db8::1]", 22, "[2001:db8::1]:22"},        // bracketed IPv6, no port — was double-bracketed before the fix
		{"[2001:db8::1]:2222", 22, "[2001:db8::1]:2222"}, // bracketed IPv6, explicit port wins
	}
	for _, c := range cases {
		m := New(c.addr, WithPort(c.port))
		if got := m.dialAddr(); got != c.want {
			t.Errorf("dialAddr(%q, port=%d) = %q, want %q", c.addr, c.port, got, c.want)
		}
	}
}

func TestTwoInstancesGetDistinctIDs(t *testing.T) {
	a := New("host-a")
	b := New("host-b")
	if a.id == b.id {
		t.Errorf("two Models got the same id: %d", a.id)
	}
}

func TestUniqueOptionErrorSurfacesAtInit(t *testing.T) {
	m := New("example.com", WithPrivateKeyFile("/nonexistent/path/id_ed25519", ""))
	if m.state != stateError {
		t.Fatalf("state = %v, want stateError after a bad option", m.state)
	}
	if m.setupErr == nil {
		t.Fatal("setupErr = nil, want an error about the missing key file")
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil Cmd, want one that delivers errMsg")
	}
	msg := cmd()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("Init()'s Cmd produced %T, want errMsg", msg)
	}
	if em.id != m.id {
		t.Errorf("errMsg.id = %d, want %d", em.id, m.id)
	}
}