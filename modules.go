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
type ModuleMeta struct {
	Summary string `json:"summary"`
	Size    string `json:"size"`
	Apps    []App  `json:"apps"`
	// Absent means true. Only consulted for an unattended install; the picker
	// still preselects everything, because there someone can say no.
	Default *bool `json:"default,omitempty"`
}

// automatic is what an unattended install enables: every module that did not
// opt out with `meta.default = false`. The list lives in the config repo, not
// here -- box does not know what any module contains.
func automatic(meta map[string]ModuleMeta) []string {
	out := []string{}
	for _, n := range sortedKeys(meta) {
		if d := meta[n].Default; d != nil && !*d {
			continue
		}
		out = append(out, n)
	}
	return out
}

// App is one program a module installs, with a link to where its releases live.
// Rendered as an OSC 8 hyperlink in the picker.
type App struct {
	Name string `json:"name"`
	URL  string `json:"url"`
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
		if conf.ModulesUnset {
			conf.Modules = sortedKeys(meta)
		}

		switch {
		case len(rest) == 0:
			if !isTTY() {
				return listModules(meta, conf.Modules)
			}
			selected, err := pickModules(meta, conf.Modules)
			if err != nil {
				return err
			}
			if sameSet(selected, conf.Modules) {
				note("no change")
				return nil
			}
			conf.Modules = selected
		case rest[0] == "list":
			return listModules(meta, conf.Modules)
		case rest[0] == "enable" || rest[0] == "disable":
			enable := rest[0] == "enable"
			names := rest[1:]
			if len(names) == 0 {
				return fmt.Errorf("usage: box modules %s NAME...", rest[0])
			}
			for _, n := range names {
				if _, ok := meta[n]; !ok {
					return fmt.Errorf("unknown module %q\n       known: %s",
						n, strings.Join(sortedKeys(meta), ", "))
				}
			}
			for _, n := range names {
				conf.Modules = setMembership(conf.Modules, n, enable)
			}
		default:
			return fmt.Errorf("unknown argument %q\n       usage: box modules [list|enable NAME...|disable NAME...]", rest[0])
		}

		if err := e.writeLocalConf(conf); err != nil {
			return err
		}
		okay("modules: " + strings.Join(conf.Modules, ", "))
		return e.apply(applyOpts{})
	},
}

func listModules(meta map[string]ModuleMeta, enabled []string) error {
	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}
	var b strings.Builder
	for _, n := range sortedKeys(meta) {
		m := meta[n]
		mark := mutedStyle.Render("  off")
		if on[n] {
			mark = okStyle.Render("   on")
		}
		fmt.Fprintf(&b, "%s  %-14s %-9s %s\n",
			mark, accentStyle.Render(n), mutedStyle.Render(m.Size), m.Summary)
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
