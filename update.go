package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func (e *Env) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", e.ConfDir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Passed to the re-executed binary so the new release does not check itself again.
const flagSkipSelf = "--skip-self"

var cmdUpdate = &command{
	name:    "update",
	summary: "update box and the configuration, then rebuild",
	long: `Brings this machine to the latest state:

  1. replaces box with the latest release
  2. pulls the configuration clone (fast-forward only)
  3. rebuilds the sandbox, as 'box apply' does

Package versions come from flake.lock in the configuration repo. To move them
forward, run 'nix flake update' in a checkout of that repo and push; every machine
picks the new versions up on its next 'box update'.

A source build of box (version "dev") is not replaced by a release.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		skipSelf := false
		for _, a := range rest {
			switch a {
			case flagSkipSelf:
				skipSelf = true
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}
		if err := e.requireConf(); err != nil {
			return err
		}

		step(1, 3, "updating box")
		switch {
		case skipSelf:
			okay(version)
		case version == "dev":
			note("source build; not replacing it with a release")
		default:
			self, updated, err := replaceSelf()
			if err != nil {
				return fmt.Errorf("updating box: %w", err)
			}
			if updated {
				// The rest must run as the new release, not as this old process.
				argv := append([]string{self, "update", flagSkipSelf}, args...)
				return syscall.Exec(self, argv, os.Environ())
			}
		}

		step(2, 3, "pulling the configuration")
		before, _ := e.git("rev-parse", "--short", "HEAD")
		if out, err := e.git("pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull in %s failed:\n%s\n       resolve it there, then rerun 'box update'",
				e.ConfDir, out)
		}
		after, _ := e.git("rev-parse", "--short", "HEAD")
		if before == after {
			okay("already at " + after)
		} else {
			okay(before + " -> " + after)
		}

		step(3, 3, "rebuilding")
		return e.apply(applyOpts{})
	},
}

// replaceSelf swaps the running binary for the latest release if that is newer.
// Returns the binary's path and whether it changed.
func replaceSelf() (string, bool, error) {
	self, err := os.Executable()
	if err != nil {
		return "", false, err
	}
	self, _ = filepath.EvalSymlinks(self)

	tag, url, err := latestAsset()
	if err != nil {
		return "", false, err
	}
	if tag == version {
		okay("already on " + version)
		return self, false, nil
	}
	note(fmt.Sprintf("%s -> %s", version, tag))

	// Beside the target, so the rename is atomic on the same filesystem.
	tmp := self + ".new"
	if err := download(url, tmp); err != nil {
		os.Remove(tmp)
		return "", false, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", false, err
	}
	// Fine on Linux: the old inode stays alive for this process.
	if err := os.Rename(tmp, self); err != nil {
		os.Remove(tmp)
		return "", false, err
	}
	okay(self)
	return self, true, nil
}
