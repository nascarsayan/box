package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type applyOpts struct {
	// reconfigure re-runs the interactive form even if local.json already exists.
	reconfigure bool
}

// Does not shell out to the home-manager CLI: nix build + run the activate script
// is what `switch` does, and needs nothing on PATH before the first apply.
func (e *Env) apply(o applyOpts) error {
	if err := e.requireConf(); err != nil {
		return err
	}

	const total = 3
	step(1, total, "building the generation")
	note("first build downloads from cache.nixos.org; later ones are incremental")
	gen, err := e.nixBuild("homeConfigurations.sandbox.activationPackage")
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	okay(gen)

	step(2, total, "activating")
	// Same as `switch -b bak`: without it, activation aborts on any pre-existing file.
	if err := e.runInChroot(os.Stderr, os.Stderr,
		"env", "HOME_MANAGER_BACKUP_EXT=bak", filepath.Join(gen, "activate")); err != nil {
		return fmt.Errorf("activation failed: %w", err)
	}
	okay("generation activated")

	step(3, total, "installing the entry point")
	if err := e.installSelf(); err != nil {
		return err
	}
	return nil
}

// No --impure anywhere: machine facts are resolved here and written to local.json.
func (e *Env) nixBuild(attr string) (string, error) {
	out, err := e.capture("nix",
		"--extra-experimental-features", "nix-command flakes",
		"build", "--no-link", "--print-out-paths",
		e.flakeRef()+"#"+attr)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("nix build printed no store path for %s", attr)
	}
	return fields[len(fields)-1], nil
}

// The sandbox needs its own copy: /nix is not mounted outside the namespace, and a
// binary in the store could not delete the store.
func (e *Env) installSelf() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := e.boxBin()
	if same, _ := sameFile(self, dst); same {
		okay(dst + " (already current)")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	// Rename, not overwrite: in-place fails with ETXTBSY while a shell is running.
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	okay(dst)
	return nil
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

var cmdApply = &command{
	name:    "apply",
	summary: "rebuild the sandbox from the configuration",
	long: `Builds and activates the current configuration. Run it after editing anything
in the config repo, or after 'box modules'.

Equivalent to, and interchangeable with:

  cd ~/.config/box && home-manager switch --flake .#sandbox

box does the same two steps directly -- nix build on the activation package, then
run its activate script -- so nothing needs home-manager on PATH beforehand.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		o := applyOpts{}
		for _, a := range rest {
			switch a {
			case "--reconfigure":
				o.reconfigure = true
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}
		if o.reconfigure {
			conf, err := e.readLocalConf()
			if err != nil {
				return err
			}
			meta, err := e.moduleMeta()
			if err != nil {
				return err
			}
			if err := e.configureForm(conf, meta); err != nil {
				return err
			}
			if err := e.writeLocalConf(conf); err != nil {
				return err
			}
		}
		return e.apply(o)
	},
}
