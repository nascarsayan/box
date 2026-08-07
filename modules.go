package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Mirrors the `meta` attrset each box-config/modules file declares. box has no
// built-in list: it asks nix. Cheap because `meta` never touches pkgs, so nixpkgs is
// not instantiated to print descriptions.
// A module is a heading, not a unit of installation: it groups apps and gives
// them a shared summary. What gets installed is chosen per app.
type ModuleMeta struct {
	Summary string `json:"summary"`
	Apps    []App  `json:"apps"`
}

// App is one program, with a link to where its releases live -- rendered as an
// OSC 8 hyperlink in the picker.
type App struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size string `json:"size"`
	// Absent means true. Consulted only for an unattended install; the picker
	// preselects everything, because there someone can say no.
	Default *bool `json:"default,omitempty"`
}

// allApps is every app across every module, in module then declaration order.
func allApps(meta map[string]ModuleMeta) []string {
	out := []string{}
	for _, m := range sortedKeys(meta) {
		for _, a := range meta[m].Apps {
			out = append(out, a.Name)
		}
	}
	return out
}

// automatic is what an unattended install enables: every app that did not opt
// out with `default = false`. The list lives in the config repo -- box does not
// know what any app is.
func automatic(meta map[string]ModuleMeta) []string {
	out := []string{}
	for _, m := range sortedKeys(meta) {
		for _, a := range meta[m].Apps {
			if a.Default != nil && !*a.Default {
				continue
			}
			out = append(out, a.Name)
		}
	}
	return out
}

// appsForModules expands module names to their apps, which is what
// `--modules kubernetes` has to mean now that selection is per app.
func appsForModules(meta map[string]ModuleMeta, mods []string) ([]string, error) {
	out := []string{}
	for _, m := range mods {
		mm, ok := meta[m]
		if !ok {
			return nil, fmt.Errorf("unknown module %q\n       known: %s",
				m, strings.Join(sortedKeys(meta), ", "))
		}
		for _, a := range mm.Apps {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

// moduleOf reports which module declares an app, for error messages.
func moduleOf(meta map[string]ModuleMeta, app string) string {
	for _, m := range sortedKeys(meta) {
		for _, a := range meta[m].Apps {
			if a.Name == app {
				return m
			}
		}
	}
	return ""
}

func (e *Env) moduleMeta() (map[string]ModuleMeta, error) {
	out, err := e.capture("nix", "--extra-experimental-features", "nix-command flakes",
		"eval", e.flakeRef()+"#moduleMeta", "--json")
	if err != nil {
		return nil, fmt.Errorf("reading module metadata from %s: %w", e.ConfDir, err)
	}
	var m map[string]ModuleMeta
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil, fmt.Errorf("module metadata is not valid JSON: %w", err)
	}
	return m, nil
}

var cmdModules = &command{
	name:    "modules",
	args:    "[enable NAME...] [disable NAME...]",
	summary: "browse and toggle optional modules",
	long: `With no arguments, opens a picker listing every module with its description,
approximate size and current state. Space toggles, enter applies.

  box modules                     interactive picker
  box modules enable kubernetes   non-interactive, scriptable
  box modules disable ai
  box modules list                print the table and exit

Modules are not defined here. Each one is a file in the configuration repo
declaring its own metadata, and box reads that list with 'nix eval .#moduleMeta'.
Adding a module upstream needs no new version of box.

Toggling writes the selection to local.json and then applies it, so the sandbox
matches what the picker showed when it exits. Nothing is downloaded for a module
you turn off, and turning one off garbage-collects nothing by itself -- run
'nix store gc' inside the sandbox to reclaim the space.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		if err := e.requireConf(); err != nil {
			return err
		}

		meta, err := e.moduleMeta()
		if err != nil {
			return err
		}
		conf, err := e.readLocalConf()
		if err != nil {
			return fmt.Errorf("%v\n       run 'box init' first", err)
		}
		conf.migrateApps(meta)

		switch {
		case len(rest) == 0:
			if !isTTY() {
				return listApps(meta, conf.Apps)
			}
			selected, err := pickApps(meta, conf.Apps)
			if err != nil {
				return err
			}
			if sameSet(selected, conf.Apps) {
				note("no change")
				return nil
			}
			conf.Apps = selected
		case rest[0] == "list":
			return listApps(meta, conf.Apps)
		case rest[0] == "enable" || rest[0] == "disable":
			enable := rest[0] == "enable"
			names := rest[1:]
			if len(names) == 0 {
				return fmt.Errorf("usage: box modules %s NAME...\n"+
					"       NAME is an app, or a module to mean all of its apps", rest[0])
			}
			// A module name means every app under it -- the old spelling still
			// works, and is the obvious way to say "all the kubernetes tools".
			var targets []string
			for _, n := range names {
				if _, ok := meta[n]; ok {
					apps, err := appsForModules(meta, []string{n})
					if err != nil {
						return err
					}
					targets = append(targets, apps...)
					continue
				}
				if moduleOf(meta, n) == "" {
					return fmt.Errorf("unknown app or module %q\n       apps: %s\n       modules: %s",
						n, strings.Join(allApps(meta), ", "), strings.Join(sortedKeys(meta), ", "))
				}
				targets = append(targets, n)
			}
			for _, n := range targets {
				conf.Apps = setMembership(conf.Apps, n, enable)
			}
		default:
			return fmt.Errorf("unknown argument %q\n       usage: box modules [list|enable NAME...|disable NAME...]", rest[0])
		}

		if err := e.writeLocalConf(conf); err != nil {
			return err
		}
		okay(fmt.Sprintf("%d apps: %s", len(conf.Apps), strings.Join(conf.Apps, ", ")))
		return e.apply(applyOpts{})
	},
}

func listApps(meta map[string]ModuleMeta, enabled []string) error {
	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}
	var b strings.Builder
	for _, m := range sortedKeys(meta) {
		fmt.Fprintf(&b, "\n%s  %s\n", accentStyle.Render(m), mutedStyle.Render(meta[m].Summary))
		for _, a := range meta[m].Apps {
			mark := mutedStyle.Render("  off")
			if on[a.Name] {
				mark = okStyle.Render("   on")
			}
			fmt.Fprintf(&b, "%s  %-16s %s\n", mark, a.Name, mutedStyle.Render(a.Size))
		}
	}
	fmt.Fprint(os.Stdout, b.String())
	return nil
}

func setMembership(list []string, name string, want bool) []string {
	out := make([]string, 0, len(list)+1)
	seen := false
	for _, v := range list {
		if v == name {
			seen = true
			if !want {
				continue
			}
		}
		out = append(out, v)
	}
	if want && !seen {
		out = append(out, name)
	}
	return out
}

// subtract returns the members of a that are not in b, order preserved.
func subtract(a, b []string) []string {
	in := map[string]bool{}
	for _, v := range b {
		in[v] = true
	}
	out := []string{}
	for _, v := range a {
		if !in[v] {
			out = append(out, v)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, v := range a {
		m[v] = true
	}
	for _, v := range b {
		if !m[v] {
			return false
		}
	}
	return true
}
