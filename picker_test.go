package main

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Key handling is tested here rather than through a pty: bubbletea does not read
// piped stdin, so a scripted terminal session cannot drive it, and a test that
// silently exercises nothing is worse than no test.
func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func testMeta() map[string]ModuleMeta {
	return map[string]ModuleMeta{
		"ai":         {Summary: "agents", Size: "~2G"},
		"files":      {Summary: "files", Size: "~250M"},
		"kubernetes": {Summary: "k8s", Size: "~1G"},
	}
}

func drive(p *picker, keys ...string) {
	for _, k := range keys {
		p.Update(key(k))
	}
}

func TestCursorMoves(t *testing.T) {
	p := newPicker(testMeta(), []string{})
	drive(p, "j", "j")
	if got := p.names[p.cursor]; got != "kubernetes" {
		t.Fatalf("after jj cursor = %q, want kubernetes", got)
	}
	drive(p, "k")
	if got := p.names[p.cursor]; got != "files" {
		t.Fatalf("after k cursor = %q, want files", got)
	}
}

func TestCursorClampsAtEnds(t *testing.T) {
	p := newPicker(testMeta(), []string{})
	drive(p, "k", "k")
	if p.cursor != 0 {
		t.Fatalf("cursor went above the first row: %d", p.cursor)
	}
	drive(p, "j", "j", "j", "j")
	if p.cursor != len(p.names)-1 {
		t.Fatalf("cursor went past the last row: %d", p.cursor)
	}
}

func TestSpaceToggles(t *testing.T) {
	p := newPicker(testMeta(), []string{})
	drive(p, " ")
	if !reflect.DeepEqual(p.selected(), []string{"ai"}) {
		t.Fatalf("space did not select: %v", p.selected())
	}
	drive(p, " ")
	if len(p.selected()) != 0 {
		t.Fatalf("space did not deselect: %v", p.selected())
	}
}

func TestAllAndNone(t *testing.T) {
	p := newPicker(testMeta(), []string{})
	drive(p, "a")
	if len(p.selected()) != 3 {
		t.Fatalf("a should select every module, got %v", p.selected())
	}
	drive(p, "n")
	if len(p.selected()) != 0 {
		t.Fatalf("n should clear the selection, got %v", p.selected())
	}
}

func TestPreselection(t *testing.T) {
	p := newPicker(testMeta(), []string{"ai", "kubernetes"})
	if got := p.selected(); !reflect.DeepEqual(got, []string{"ai", "kubernetes"}) {
		t.Fatalf("preselection lost: %v", got)
	}
}

func TestEnterConfirmsAndQuitCancels(t *testing.T) {
	p := newPicker(testMeta(), []string{"ai"})
	drive(p, "enter")
	if !p.done || p.cancelled {
		t.Fatalf("enter should confirm: done=%v cancelled=%v", p.done, p.cancelled)
	}
	p = newPicker(testMeta(), []string{"ai"})
	drive(p, "q")
	if !p.cancelled {
		t.Fatal("q should cancel")
	}
}

// A zero width is what a pty with no size reports; it must not collapse the wrap
// width and break the app list after one entry.
func TestZeroWidthIgnored(t *testing.T) {
	p := newPicker(testMeta(), nil)
	p.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if p.width <= 0 {
		t.Fatalf("width clobbered to %d", p.width)
	}
	p.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if p.width != 120 {
		t.Fatalf("width = %d, want 120", p.width)
	}
}

func TestLinkIsOSC8(t *testing.T) {
	got := link("codex", "https://example.invalid")
	want := "\x1b]8;;https://example.invalid\x1b\\codex\x1b]8;;\x1b\\"
	if got != want {
		t.Fatalf("link = %q, want %q", got, want)
	}
}
