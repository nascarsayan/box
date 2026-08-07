package main

import (
	"reflect"
	"testing"
)

// The "already up to date" path is easy to hit by accident (nixos-unstable moves
// every few days), so the interesting case -- something actually changed -- is
// covered here rather than waiting for the channel to advance.
func TestDiffPins(t *testing.T) {
	before := map[string]string{
		"nixpkgs":      "1559d3daa3ec (2026-07-30)",
		"home-manager": "e8827fbbb120 (2026-07-31)",
		"tmux-local":   "d7da714a68ad (2025-10-02)",
	}
	after := map[string]string{
		"nixpkgs":      "aaaaaaaaaaaa (2026-08-05)",
		"home-manager": "e8827fbbb120 (2026-07-31)",
		"tmux-local":   "d7da714a68ad (2025-10-02)",
	}
	got := diffPins(before, after)
	want := []string{"nixpkgs          1559d3daa3ec (2026-07-30) -> aaaaaaaaaaaa (2026-08-05)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffPins = %#v, want %#v", got, want)
	}
}

func TestDiffPinsNoChange(t *testing.T) {
	m := map[string]string{"nixpkgs": "abc (2026-01-01)"}
	if got := diffPins(m, m); len(got) != 0 {
		t.Fatalf("expected no changes, got %v", got)
	}
}

// A newly added input has no "before", which must read as an addition rather than
// crash or be silently dropped.
func TestDiffPinsNewInput(t *testing.T) {
	before := map[string]string{}
	after := map[string]string{"nixpkgs": "abc (2026-01-01)"}
	got := diffPins(before, after)
	if len(got) != 1 {
		t.Fatalf("a new input should be reported, got %v", got)
	}
}

func TestAutomaticRespectsOptOut(t *testing.T) {
	no, yes := false, true
	meta := map[string]ModuleMeta{
		"ai":          {Default: &no},  // opted out
		"multiplexer": {Default: &no},  // opted out
		"kubernetes":  {},              // absent means in
		"net":         {Default: &yes}, // explicitly in
	}
	got := automatic(meta)
	want := []string{"kubernetes", "net"}
	if !sameSet(got, want) {
		t.Fatalf("automatic() = %v, want %v", got, want)
	}
	// The picker is unaffected: it still offers everything.
	if len(sortedKeys(meta)) != 4 {
		t.Fatalf("meta should still list all four modules")
	}
}

func TestSubtract(t *testing.T) {
	got := subtract([]string{"a", "b", "c"}, []string{"b"})
	if !sameSet(got, []string{"a", "c"}) {
		t.Fatalf("subtract = %v", got)
	}
	if len(subtract([]string{"a"}, []string{"a"})) != 0 {
		t.Fatalf("subtract of identical sets should be empty")
	}
}
