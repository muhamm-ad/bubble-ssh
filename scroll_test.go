package bubblessh

import (
	"strings"
	"testing"
)

func TestFitBlockKeepsBottomAndPads(t *testing.T) {
	tall := "a\nb\nc\nd\ne"
	got := fitBlock(tall, 10, 3)
	want := "c\nd\ne"
	if got != want {
		t.Errorf("tall content: got %q, want %q", got, want)
	}

	short := "hi"
	got = fitBlock(short, 10, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("short content: got %d lines, want 3: %q", len(lines), got)
	}
	if lines[0] != "hi" || lines[1] != "" || lines[2] != "" {
		t.Errorf("short content: got %q, want hi + 2 blank lines", got)
	}
}

func TestFitBlockTruncatesWidth(t *testing.T) {
	got := fitBlock("hello world", 5, 1)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}
