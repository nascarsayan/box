package main

import (
	"testing"
)

func TestAutomaticRespectsOptOut(t *testing.T) {
	no, yes := false, true
	meta := map[string]ModuleMeta{
		"ai": {Apps: []App{
			{Name: "codex", Default: &no},       // opted out
			{Name: "claude-code", Default: &no}, // opted out
		}},
		"files": {Apps: []App{
			{Name: "yazi", Default: &no}, // heavy, opted out
			{Name: "duf"},                // absent means in
		}},
		"net": {Apps: []App{
			{Name: "xh", Default: &yes}, // explicitly in
		}},
	}
	got := automatic(meta)
	want := []string{"duf", "xh"}
	if !sameSet(got, want) {
		t.Fatalf("automatic() = %v, want %v", got, want)
	}
	// The picker is unaffected: it still offers every app.
	if len(allApps(meta)) != 5 {
		t.Fatalf("allApps should list all five, got %v", allApps(meta))
	}
}

// A pre-split local.json selected whole modules. Upgrading box must install the
// same things, not silently fall back to the new default set.
func TestMigrateAppsExpandsOldModuleList(t *testing.T) {
	meta := map[string]ModuleMeta{
		"ai":  {Apps: []App{{Name: "codex"}, {Name: "claude-code"}}},
		"net": {Apps: []App{{Name: "xh"}}},
	}
	c := &LocalConf{Modules: []string{"ai"}}
	c.migrateApps(meta)
	if !sameSet(c.Apps, []string{"codex", "claude-code"}) {
		t.Fatalf("migrated to %v, want the ai apps", c.Apps)
	}
	// An explicit app list is left alone.
	c = &LocalConf{Apps: []string{"xh"}, Modules: []string{"ai"}}
	c.migrateApps(meta)
	if !sameSet(c.Apps, []string{"xh"}) {
		t.Fatalf("existing app list was overwritten: %v", c.Apps)
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
