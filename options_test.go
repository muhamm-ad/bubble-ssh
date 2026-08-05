package bubblessh

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("converting test key: %v", err)
	}
	return sshPub
}

func TestWithAcceptNewHostKeysTrustsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "known_hosts") // parent dir doesn't exist yet
	m := New("example.com", WithAcceptNewHostKeys(path))
	if m.setupErr != nil {
		t.Fatalf("unexpected setup error: %v", m.setupErr)
	}
	if m.hostKeyCallback == nil {
		t.Fatal("hostKeyCallback was not set")
	}

	addr := &net.TCPAddr{}
	firstKey := testPublicKey(t)

	// First contact: never seen before, must be trusted and remembered.
	if err := m.hostKeyCallback("example.com:22", addr, firstKey); err != nil {
		t.Fatalf("first connection to a new host should be trusted, got: %v", err)
	}

	// Same host, same key again: must still succeed (now checked strictly
	// against what was just written, and it matches).
	if err := m.hostKeyCallback("example.com:22", addr, firstKey); err != nil {
		t.Fatalf("reconnecting with the same key should succeed, got: %v", err)
	}

	// Same host, DIFFERENT key: this is the actual security property —
	// must be refused, not silently trusted again.
	secondKey := testPublicKey(t)
	if err := m.hostKeyCallback("example.com:22", addr, secondKey); err == nil {
		t.Fatal("a changed host key must be refused, got nil error")
	}
}

func TestEnsureFileExistsCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "known_hosts")
	if err := ensureFileExists(path); err != nil {
		t.Fatalf("ensureFileExists: %v", err)
	}
	if err := ensureFileExists(path); err != nil { // must also be safe to call twice
		t.Fatalf("ensureFileExists on an existing file: %v", err)
	}
}
