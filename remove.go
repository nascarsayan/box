package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var cmdRemove = &command{
	name:    "remove",
	args:    "[--yes] [--purge-config]",
	summary: "delete the sandbox and its nix store",
	long: `Removes the sandbox directory, including its nix store, and strips the shortcut
block from your shell rc files.

The configuration clone is kept by default, since it may hold module changes you
have not pushed. Add --purge-config to remove that too.

Why this needs to be a command rather than an rm: every path in a nix store is
read-only, so plain 'rm -rf' fails on the first one. box makes the store writable
first, which is the same thing nix's own uninstaller does.

Nothing outside the sandbox is touched. Passthrough symlinks point at your real
home, but only the links are removed, never their targets.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		yes, purgeConf := false, false
		for _, a := range rest {
			switch a {
			case "-y", "--yes":
				yes = true
			case "--purge-config":
				purgeConf = true
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}

		if _, err := os.Stat(e.SandboxDir); err != nil {
			return fmt.Errorf("no sandbox at %s", e.SandboxDir)
		}

		targets := []string{e.SandboxDir}
		if purgeConf {
			targets = append(targets, e.ConfDir)
		}

		fmt.Fprintln(os.Stderr, boxStyle.Render(strings.Join([]string{
			errStyle.Render("about to delete"),
			"",
			strings.Join(targets, "\n"),
			"",
			mutedStyle.Render("your real home, ws tree and ssh keys are untouched"),
		}, "\n")))

		if !yes {
			if !isTTY() {
				return fmt.Errorf("refusing to delete without a terminal; pass --yes")
			}
			fmt.Fprint(os.Stderr, "type the sandbox path to confirm: ")
			var answer string
			fmt.Scanln(&answer)
			if strings.TrimSpace(answer) != e.SandboxDir {
				return fmt.Errorf("aborted")
			}
		}

		for _, t := range targets {
			note("removing " + t)
			// safeRemoveAll, not os.RemoveAll: see saferm.go. A recursive delete that
			// meets nix-user-chroot's bind mounts walks out of the sandbox and eats
			// the host.
			if err := safeRemoveAll(t); err != nil {
				return err
			}
		}

		for _, rc := range e.rcCandidates() {
			removed, err := stripBlock(rc)
			if err == nil && removed {
				okay("removed the shortcut block from " + rc)
			}
		}
		if err := os.Remove(filepath.Join(stateHome(), stateFileName)); err == nil {
			okay("cleared " + filepath.Join(stateHome(), stateFileName))
		}
		okay("done")
		return nil
	},
}
