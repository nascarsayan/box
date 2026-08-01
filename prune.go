package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// storeSizeMB is measured with du rather than `nix path-info -S`, which reports
// closure size and double-counts paths shared between generations.
func (e *Env) storeSizeMB() int {
	out, err := exec.Command("du", "-sm", e.nixDir()).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.Fields(string(out))[0])
	return n
}

var cmdPrune = &command{
	name:    "prune",
	args:    "[--keep-generations N]",
	summary: "reclaim disk space in the nix store",
	long: `Deletes store paths nothing references any more, then hard-links identical files
together.

Worth running after changing modules: turning a module off stops it being
installed, but the paths stay on disk until something collects them.

On a fresh base install this reclaims a bit over a third -- 2848M to 1710M in a
measured run -- because a build pulls in compilers and sources that the result does
not reference.

  --keep-generations N   keep the last N home-manager generations (default 1)

Keeping fewer generations frees more but gives up rollback. The default keeps the
current one only, which is what you want on a machine you can rebuild with
'box apply'.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		keep := 1
		for i := 0; i < len(rest); i++ {
			switch {
			case rest[i] == "--keep-generations":
				if i+1 >= len(rest) {
					return fmt.Errorf("--keep-generations needs a number")
				}
				i++
				if keep, err = strconv.Atoi(rest[i]); err != nil {
					return fmt.Errorf("--keep-generations: %w", err)
				}
			case strings.HasPrefix(rest[i], "--keep-generations="):
				if keep, err = strconv.Atoi(strings.TrimPrefix(rest[i], "--keep-generations=")); err != nil {
					return fmt.Errorf("--keep-generations: %w", err)
				}
			default:
				return fmt.Errorf("unknown flag %q", rest[i])
			}
		}
		return e.prune(keep)
	},
}

func (e *Env) prune(keepGenerations int) error {
	if err := e.requireSandbox(); err != nil {
		return err
	}
	before := e.storeSizeMB()

	step(1, 2, "collecting garbage")
	// +N keeps the N most recent; nix-env counts the current one among them.
	if err := e.runInChrootBare(os.Stderr, "/bin/sh", "-c",
		fmt.Sprintf(`. "$HOME/.nix-profile/etc/profile.d/nix.sh" 2>/dev/null
nix-env --delete-generations +%d 2>/dev/null || true
nix-collect-garbage --delete-old 2>&1 | tail -2`, keepGenerations)); err != nil {
		return fmt.Errorf("garbage collection failed: %w", err)
	}

	step(2, 2, "hard-linking duplicates")
	if err := e.runInChrootBare(os.Stderr, "/bin/sh", "-c",
		`. "$HOME/.nix-profile/etc/profile.d/nix.sh" 2>/dev/null
nix --extra-experimental-features nix-command store optimise 2>&1 | tail -1`); err != nil {
		return fmt.Errorf("optimise failed: %w", err)
	}

	after := e.storeSizeMB()
	if before > 0 && after > 0 {
		okay(fmt.Sprintf("%dM -> %dM (reclaimed %dM)", before, after, before-after))
	}
	return nil
}
