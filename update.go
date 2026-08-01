package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// lockPins maps input name -> a short human description of what it is pinned to.
// Read from flake.lock directly rather than by evaluating the flake, so it works
// before and after an update without building anything.
func (e *Env) lockPins() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(e.ConfDir, "flake.lock"))
	if err != nil {
		return nil, err
	}
	var lock struct {
		Nodes map[string]struct {
			Locked struct {
				Rev          string `json:"rev"`
				LastModified int64  `json:"lastModified"`
			} `json:"locked"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &lock); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, n := range lock.Nodes {
		if name == "root" || n.Locked.Rev == "" {
			continue
		}
		when := ""
		if n.Locked.LastModified > 0 {
			when = time.Unix(n.Locked.LastModified, 0).UTC().Format("2006-01-02")
		}
		out[name] = fmt.Sprintf("%s (%s)", n.Locked.Rev[:12], when)
	}
	return out, nil
}

func diffPins(before, after map[string]string) []string {
	var changed []string
	for _, name := range sortedKeys(after) {
		if before[name] != after[name] {
			changed = append(changed, fmt.Sprintf("%-16s %s -> %s", name, before[name], after[name]))
		}
	}
	sort.Strings(changed)
	return changed
}

func (e *Env) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", e.ConfDir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

var cmdUpdate = &command{
	name:    "update",
	args:    "[--input NAME] [--no-apply]",
	summary: "move the pinned package versions forward",
	long: `Updates flake.lock in your configuration clone, then rebuilds.

Versions live entirely in that lock file, so this is the only thing that changes
what you have installed. 'box apply' on its own never moves a version -- that is
deliberate, and it is what makes the same config reproduce identically on another
machine until someone updates the lock.

  box update                    every input, including nixpkgs
  box update --input nixpkgs    just one
  box update --no-apply         update the lock, rebuild later

Afterwards, 'box contribute' opens a chore PR with the new lock, so everyone else
gets the versions you have already run.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		input, noApply := "", false
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case a == "--input":
				if i+1 >= len(rest) {
					return fmt.Errorf("--input needs a name")
				}
				i++
				input = rest[i]
			case strings.HasPrefix(a, "--input="):
				input = strings.TrimPrefix(a, "--input=")
			case a == "--no-apply":
				noApply = true
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}
		if err := e.requireConf(); err != nil {
			return err
		}

		before, err := e.lockPins()
		if err != nil {
			return fmt.Errorf("reading flake.lock: %w", err)
		}

		step(1, 2, "updating the lock")
		argv := []string{"nix", "--extra-experimental-features", "nix-command flakes",
			"flake", "update"}
		if input != "" {
			argv = append(argv, input)
		}
		argv = append(argv, "--flake", e.flakeRef())
		if err := e.runInChroot(os.Stderr, os.Stderr, argv...); err != nil {
			return fmt.Errorf("flake update failed: %w", err)
		}

		after, err := e.lockPins()
		if err != nil {
			return err
		}
		changed := diffPins(before, after)
		if len(changed) == 0 {
			okay("already up to date")
			return nil
		}
		for _, c := range changed {
			note(c)
		}

		if noApply {
			okay("lock updated; run 'box apply' when ready")
			return nil
		}

		step(2, 2, "applying")
		if err := e.apply(applyOpts{}); err != nil {
			return fmt.Errorf("%w\n       the lock was updated but the build failed; "+
				"`git -C %s checkout flake.lock` to go back", err, e.ConfDir)
		}
		okay("updated and applied — 'box contribute' to send the lock upstream")
		return nil
	},
}

var cmdContribute = &command{
	name:    "contribute",
	args:    "[--title TEXT]",
	summary: "open a PR with the package versions you are running",
	long: `Pushes your updated flake.lock as a chore PR against the configuration repo.

The point of this: you have already run these versions on a real machine. Everyone
else gets them only if the lock goes back upstream.

Only flake.lock is committed. Your own settings live in local.json, which is
gitignored, so there is nothing personal to leak into the diff -- that split is
why the config repo is a clone rather than a fetch.

Needs the gh CLI, authenticated. If you cannot push to the repo, gh will offer to
fork it first.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		title := ""
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case a == "--title":
				if i+1 >= len(rest) {
					return fmt.Errorf("--title needs text")
				}
				i++
				title = rest[i]
			case strings.HasPrefix(a, "--title="):
				title = strings.TrimPrefix(a, "--title=")
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}
		if err := e.requireConf(); err != nil {
			return err
		}
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("gh is not on PATH; install it or open the PR by hand")
		}

		// Only the lock. Anything else that is dirty is the contributor's own work
		// and does not belong in a version-bump PR.
		status, _ := e.git("status", "--porcelain", "--", "flake.lock")
		if status == "" {
			return fmt.Errorf("flake.lock is unchanged\n       run 'box update' first")
		}

		pins, err := e.lockPins()
		if err != nil {
			return err
		}
		if title == "" {
			title = "chore: update flake.lock"
			if nixpkgs, ok := pins["nixpkgs"]; ok {
				if i := strings.Index(nixpkgs, "("); i > 0 {
					title = "chore: update flake.lock (nixpkgs " +
						strings.Trim(nixpkgs[i:], "()") + ")"
				}
			}
		}

		branch := "chore/flake-lock-" + time.Now().UTC().Format("2006-01-02-150405")
		step(1, 3, "committing flake.lock on "+branch)
		if out, err := e.git("checkout", "-b", branch); err != nil {
			return fmt.Errorf("creating the branch: %s", out)
		}
		if out, err := e.git("add", "flake.lock"); err != nil {
			return fmt.Errorf("staging: %s", out)
		}
		body := "Ran these versions on " + hostname() + " before opening this.\n\n```\n" +
			strings.Join(sortedPins(pins), "\n") + "\n```\n"
		if out, err := e.git("commit", "-m", title, "-m", body); err != nil {
			return fmt.Errorf("committing: %s", out)
		}
		okay(branch)

		step(2, 3, "pushing")
		if out, err := e.git("push", "-u", "origin", branch); err != nil {
			note("cannot push to origin: " + firstLine(out))
			note("gh will offer to fork; answer its prompts")
		}

		step(3, 3, "opening the pull request")
		cmd := exec.Command("gh", "pr", "create", "--title", title, "--body", body)
		cmd.Dir = e.ConfDir
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh pr create: %w", err)
		}
		return nil
	},
}

func sortedPins(pins map[string]string) []string {
	out := make([]string, 0, len(pins))
	for _, k := range sortedKeys(pins) {
		out = append(out, fmt.Sprintf("%-16s %s", k, pins[k]))
	}
	return out
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "a machine"
	}
	return h
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
