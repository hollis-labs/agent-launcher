package main

import "testing"

func TestParsePairs(t *testing.T) {
	got := parsePairs([]string{"project=tether", "runner=claude-code", "bad-no-eq"})
	if got["project"] != "tether" {
		t.Fatalf("project = %q", got["project"])
	}
	if got["runner"] != "claude-code" {
		t.Fatalf("runner = %q", got["runner"])
	}
	if _, ok := got["bad-no-eq"]; ok {
		t.Fatal("a value with no '=' should be skipped")
	}
}

func TestExpandHome(t *testing.T) {
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Fatalf("absolute path should pass through, got %q", got)
	}
	if got := expandHome(""); got != "" {
		t.Fatalf("empty should pass through, got %q", got)
	}
	if got := expandHome("~/x"); got == "~/x" {
		t.Fatal("~/x should have expanded")
	}
}
