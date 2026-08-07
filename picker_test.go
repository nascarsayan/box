package main

import (
	"reflect"
	"strings"
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

// Rows come out as: ai, claude-code, codex, files, yazi, duf, kubernetes, kubectl.
// Headings included -- the cursor walks over them too.
func testMeta() map[string]ModuleMeta {
	no := false
	return map[string]ModuleMeta{
		"ai": {Summary: "agents", Apps: []App{
			{Name: "claude-code", Size: "~270M", Default: &no},
			{Name: "codex", Size: "~490M", Default: &no},
		}},
		"files": {Summary: "files", Apps: []App{
			{Name: "yazi", Size: "~1.4G", Default: &no},
			{Name: "duf", Size: "~10M"},
		}},
		"kubernetes": {Summary: "k8s", Apps: []App{
			{Name: "kubectl", Size: "~55M"},
		}},
	}
}

func drive(p *picker, keys ...string) {
	for _, k := range keys {
		p.Update(key(k))
	}
}

// The cursor starts on the first app, not the heading above it: landing on a
// heading would make the first keypress toggle a whole group.
func TestCursorStartsOnFirstApp(t *testing.T) {
	p := newPicker(testMeta(), nil)
	r := p.rows[p.cursor]
	if r.app == nil || r.app.Name != "claude-code" {
		t.Fatalf("cursor started on %+v, want app claude-code", r)
	}
}

func TestCursorMovesOverHeadingsAndApps(t *testing.T) {
	p := newPicker(testMeta(), nil)
	drive(p, "j") // codex
	if got := p.rows[p.cursor].app.Name; got != "codex" {
		t.Fatalf("after j = %q, want codex", got)
	}
	drive(p, "j") // files heading
	if r := p.rows[p.cursor]; r.app != nil || r.module != "files" {
		t.Fatalf("expected the files heading, got %+v", r)
	}
	drive(p, "k")
	if got := p.rows[p.cursor].app.Name; got != "codex" {
		t.Fatalf("after k = %q, want codex", got)
	}
}

func TestCursorClampsAtEnds(t *testing.T) {
	p := newPicker(testMeta(), nil)
	drive(p, "k", "k", "k")
	if p.cursor != 0 {
		t.Fatalf("cursor went above the first row: %d", p.cursor)
	}
	for i := 0; i < len(p.rows)+3; i++ {
		drive(p, "j")
	}
	if p.cursor != len(p.rows)-1 {
		t.Fatalf("cursor went past the last row: %d", p.cursor)
	}
}

// The point of the rewrite: one app at a time, without its neighbours.
func TestSpaceTogglesOneApp(t *testing.T) {
	p := newPicker(testMeta(), nil)
	drive(p, " ")
	if !reflect.DeepEqual(p.selected(), []string{"claude-code"}) {
		t.Fatalf("space selected %v, want just claude-code", p.selected())
	}
	drive(p, " ")
	if len(p.selected()) != 0 {
		t.Fatalf("space did not deselect: %v", p.selected())
	}
}

// On a heading, space is a shortcut for the whole group -- and toggling it when
// the group is only partly on turns all of it on, not off.
func TestSpaceOnHeadingTogglesGroup(t *testing.T) {
	p := newPicker(testMeta(), nil)
	p.cursor = 0 // ai heading
	drive(p, " ")
	if got := p.selected(); !reflect.DeepEqual(got, []string{"claude-code", "codex"}) {
		t.Fatalf("heading toggle selected %v, want both ai apps", got)
	}
	drive(p, " ")
	if len(p.selected()) != 0 {
		t.Fatalf("second heading toggle should clear the group, got %v", p.selected())
	}
	// Partly on -> all on.
	p = newPicker(testMeta(), []string{"codex"})
	p.cursor = 0
	drive(p, " ")
	if got := p.selected(); !reflect.DeepEqual(got, []string{"claude-code", "codex"}) {
		t.Fatalf("partial group should fill, got %v", got)
	}
}

func TestGroupMarkShowsMixed(t *testing.T) {
	p := newPicker(testMeta(), []string{"codex"})
	if got := stripANSI(p.groupMark("ai")); got != "[~]" {
		t.Fatalf("partly-selected group mark = %q, want [~]", got)
	}
	p = newPicker(testMeta(), []string{"claude-code", "codex"})
	if got := stripANSI(p.groupMark("ai")); got != "[x]" {
		t.Fatalf("full group mark = %q, want [x]", got)
	}
	p = newPicker(testMeta(), nil)
	if got := stripANSI(p.groupMark("ai")); got != "[ ]" {
		t.Fatalf("empty group mark = %q, want [ ]", got)
	}
}

func TestAllAndNone(t *testing.T) {
	p := newPicker(testMeta(), nil)
	drive(p, "a")
	if len(p.selected()) != 5 {
		t.Fatalf("a should select every app, got %v", p.selected())
	}
	drive(p, "n")
	if len(p.selected()) != 0 {
		t.Fatalf("n should clear the selection, got %v", p.selected())
	}
}

func TestPreselection(t *testing.T) {
	p := newPicker(testMeta(), []string{"codex", "kubectl"})
	if got := p.selected(); !reflect.DeepEqual(got, []string{"codex", "kubectl"}) {
		t.Fatalf("preselection lost: %v", got)
	}
}

func TestEnterConfirmsAndQuitCancels(t *testing.T) {
	p := newPicker(testMeta(), []string{"codex"})
	drive(p, "enter")
	if !p.done || p.cancelled {
		t.Fatalf("enter should confirm: done=%v cancelled=%v", p.done, p.cancelled)
	}
	p = newPicker(testMeta(), []string{"codex"})
	drive(p, "q")
	if !p.cancelled {
		t.Fatal("q should cancel")
	}
}

// A zero width is what a pty with no size reports; it must not collapse the
// layout.
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

// View must not panic on any cursor position, headings included.
func TestViewRendersEveryCursorPosition(t *testing.T) {
	p := newPicker(testMeta(), []string{"duf"})
	for i := range p.rows {
		p.cursor = i
		if p.View() == "" {
			t.Fatalf("empty view at row %d", i)
		}
	}
}

// groupMark styles its output; compare the glyphs, not the escapes.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
