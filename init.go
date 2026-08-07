package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
)

const (
	nucVersion = "2.1.1"
	// Verified, not trusted: this binary establishes the namespace.
	nucSHA256AMD64 = "e6daa6a036e00b939531e5c11e6dcd891140813a09964fd12a64112de500fc15"
	nucSHA256ARM64 = "9aeaa70f4fb645afb343b0417e494b0271457a8cb465c4ac82b6c3b19040d397"

	defaultConfRepo = "https://github.com/nascarsayan/box-config.git"
	nixInstallURL   = "https://nixos.org/nix/install"
)

var cmdInit = &command{
	name:    "init",
	args:    "[-c DIR] [-d DIR] [--repo URL] [-y] [--apps LIST] [--modules LIST] [--no-rc]",
	summary: "create the sandbox: nix, the config clone, then apply",
	long: `Sets up everything, from nothing, without root:

  1. checks unprivileged user namespaces are available (the one hard requirement)
  2. downloads a checksum-pinned nix-user-chroot
  3. installs nix into <sandbox>/nix with the official single-user installer
  4. clones the configuration repo to <confDir>, default ~/.config/box
  5. opens the module picker, with everything preselected
  6. builds and activates the configuration
  7. adds the , shortcuts to your shell rc

Safe to re-run: each step is skipped if already done, so it doubles as a repair.

  -c, --conf-dir DIR     where to clone the configuration (default ~/.config/box)
  -d, --sandbox-dir DIR  where nix and $HOME-inside live (default ~/sandbox)
      --repo URL         clone a fork instead
      -y, --yes          never prompt; install every module
          --non-interactive, --automated   same as --yes
      --no-rc            do not touch any shell rc file
      --apps LIST        comma-separated apps to install; '' for none
      --modules LIST     shorthand for every app in those modules

Put the sandbox on a local disk. The nix store is several GB and a store on
NFS or EFS is an order of magnitude slower to build in.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		repo := defaultConfRepo
		yes := os.Getenv("BOX_ASSUME_YES") == "1"
		noRC := false
		modules := ""
		modulesSet := false
		apps := ""
		appsSet := false
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case a == "--repo":
				if i+1 >= len(rest) {
					return fmt.Errorf("--repo needs a URL")
				}
				i++
				repo = rest[i]
			case strings.HasPrefix(a, "--repo="):
				repo = strings.TrimPrefix(a, "--repo=")
			case a == "-y" || a == "--yes" || a == "--non-interactive" || a == "--automated":
				yes = true
			case a == "--no-rc":
				noRC = true
			case a == "--modules":
				if i+1 >= len(rest) {
					return fmt.Errorf("--modules needs a list (use '' for none)")
				}
				i++
				modules, modulesSet = rest[i], true
			case strings.HasPrefix(a, "--modules="):
				modules, modulesSet = strings.TrimPrefix(a, "--modules="), true
			case a == "--apps":
				if i+1 >= len(rest) {
					return fmt.Errorf("--apps needs a list (use '' for none)")
				}
				i++
				apps, appsSet = rest[i], true
			case strings.HasPrefix(a, "--apps="):
				apps, appsSet = strings.TrimPrefix(a, "--apps="), true
			default:
				return fmt.Errorf("unknown flag %q", a)
			}
		}
		interactive := !yes && isTTY()

		fmt.Fprintln(os.Stderr, boxStyle.Render(strings.Join([]string{
			titleStyle.Render("box"),
			mutedStyle.Render("rootless dev sandbox: nix-user-chroot + home-manager"),
			"",
			mutedStyle.Render("sandbox  ") + e.SandboxDir,
			mutedStyle.Render("config   ") + e.ConfDir,
		}, "\n")))

		const total = 6
		step(1, total, "checking prerequisites")
		if ok, why := userNSAvailable(); !ok {
			return fmt.Errorf("unprivileged user namespaces are unavailable.\n       %s", why)
		}
		okay("user namespaces available")
		if bad := checkDevNodes(); len(bad) > 0 {
			return fmt.Errorf("this host's /dev is broken: %s\n%s",
				strings.Join(bad, "; "), devNodeFix())
		}

		for _, d := range []string{"bin", "nix", "tmp", "run"} {
			if err := os.MkdirAll(filepath.Join(e.SandboxDir, d), 0o755); err != nil {
				return err
			}
		}
		// nix/ especially: nix-user-chroot panics if it is missing.

		step(2, total, "installing nix-user-chroot")
		if err := e.installChroot(); err != nil {
			return err
		}

		step(3, total, "installing nix")
		if err := e.installNix(); err != nil {
			return err
		}

		step(4, total, "fetching the configuration")
		if err := e.cloneConf(repo); err != nil {
			return err
		}

		step(5, total, "recording this machine's configuration")
		meta, err := e.moduleMeta()
		if err != nil {
			return err
		}
		conf, err := e.readLocalConf()
		fresh := err != nil
		if fresh {
			// Everything preselected on a fresh install; the picker is where you
			// take things out.
			conf = e.defaultLocalConf(allApps(meta))
		} else {
			conf.migrateApps(meta)
		}
		if modulesSet {
			// --modules names groups; --apps names individual programs. Both end
			// up in the same list, because selection is per app now.
			apps, err := appsForModules(meta, splitList(modules))
			if err != nil {
				return err
			}
			conf.Apps = apps
		}
		if appsSet {
			conf.Apps = splitList(apps)
			for _, n := range conf.Apps {
				if moduleOf(meta, n) == "" {
					return fmt.Errorf("unknown app %q\n       known: %s",
						n, strings.Join(allApps(meta), ", "))
				}
			}
		}
		chosen := modulesSet || appsSet
		if interactive && !chosen {
			if err := e.configureForm(conf, meta); err != nil {
				return err
			}
		} else {
			// Nobody is here to decline, so apps that opted out of unattended
			// installs stay out. Only on a first install or an unset list --
			// re-running init must not silently drop what someone chose before.
			// An explicit --apps/--modules always wins over this.
			if !chosen && (fresh || conf.AppsUnset) {
				conf.Apps = automatic(meta)
				if skipped := subtract(allApps(meta), conf.Apps); len(skipped) > 0 {
					note("not installed by default: " + strings.Join(skipped, ", ") +
						" (add with --apps, or `box modules`)")
				}
			}
			note("non-interactive: deriving every answer")
		}
		if err := e.writeLocalConf(conf); err != nil {
			return err
		}
		okay(e.localConfPath())
		if err := e.saveState(); err != nil {
			return err
		}

		step(6, total, "applying")
		if err := e.apply(applyOpts{}); err != nil {
			return err
		}

		note("reclaiming build leftovers")
		if err := e.prune(1); err != nil {
			note("prune skipped: " + err.Error())
		}

		var touched []string
		rcs := e.rcCandidates()
		if noRC {
			rcs = nil
			note("--no-rc: leaving your shell rc files alone")
		}
		for _, rc := range rcs {
			changed, err := upsertBlock(rc, e.rcBlock())
			if err != nil {
				warn(fmt.Sprintf("%s: %v", rc, err))
				continue
			}
			if changed {
				okay("shortcuts added to " + rc)
			}
			touched = append(touched, rc)
		}

		lines := []string{
			titleStyle.Render("ready"),
			"",
			mutedStyle.Render("enter     ") + e.boxBin() + mutedStyle.Render("   (or just: ,)"),
			mutedStyle.Render("modules   ") + "box modules",
			mutedStyle.Render("rebuild   ") + "box apply",
			mutedStyle.Render("enabled   ") + strings.Join(conf.Modules, ", "),
		}
		if len(touched) > 0 {
			lines = append(lines, mutedStyle.Render("shortcuts ")+", ,, ,gwq ,rv ,gcw "+
				mutedStyle.Render("(open a new shell)"))
		}
		fmt.Fprintln(os.Stderr, boxStyle.Render(strings.Join(lines, "\n")))
		return nil
	},
}

// Only the module picker, plus git identity when it could not be read from the
// real account's config. Everything else is derived; asking about it would be
// friction for no decision.
func (e *Env) configureForm(c *LocalConf, meta map[string]ModuleMeta) error {
	if c.Git.UserName == "" || c.Git.UserEmail == "" {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("git author name").Value(&c.Git.UserName),
			huh.NewInput().Title("git author email").Value(&c.Git.UserEmail),
		).Title("git identity")).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
			return err
		}
	}
	selected, err := pickApps(meta, c.Apps)
	if err != nil {
		return err
	}
	c.Apps = selected
	return nil
}

func (e *Env) installChroot() error {
	dst := e.chrootBin()
	if _, err := os.Stat(dst); err == nil {
		okay("already present")
		return nil
	}
	arch := "x86_64"
	sum := nucSHA256AMD64
	if isARM64() {
		arch, sum = "aarch64", nucSHA256ARM64
	}
	url := fmt.Sprintf(
		"https://github.com/nix-community/nix-user-chroot/releases/download/%s/nix-user-chroot-bin-%s-%s-unknown-linux-musl",
		nucVersion, nucVersion, arch)

	tmp := dst + ".new"
	if err := download(url, tmp); err != nil {
		return fmt.Errorf("downloading nix-user-chroot: %w", err)
	}
	got, err := sha256File(tmp)
	if err != nil {
		return err
	}
	if got != sum {
		os.Remove(tmp)
		return fmt.Errorf("nix-user-chroot checksum mismatch\n       expected %s\n       got      %s", sum, got)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	okay(dst)
	return nil
}

func (e *Env) installNix() error {
	// Both, not just the store: a failed install leaves /nix/store populated but no
	// profile, and skipping on the store alone would then fail further along with a
	// far less obvious error.
	_, storeErr := os.Stat(filepath.Join(e.nixDir(), "store"))
	_, nixErr := os.Stat(filepath.Join(e.SandboxDir, ".nix-profile", "bin", "nix"))
	if storeErr == nil && nixErr == nil {
		okay("nix already installed")
		return nil
	}
	if storeErr == nil && nixErr != nil {
		note("store present but no profile; re-running the installer over it")
	}
	installer := filepath.Join(e.tmpDir(), "nix-install.sh")
	if err := download(nixInstallURL, installer); err != nil {
		return fmt.Errorf("downloading the nix installer: %w", err)
	}
	defer os.Remove(installer)

	// --no-daemon: multi-user wants systemd and a build-user group, i.e. root.
	note("this downloads ~25MB and takes a minute")
	// --no-channel-add: the installer otherwise unpacks the nixpkgs channel into
	// ~/.nix-defexpr, 333M that nothing reads -- every path here goes through the
	// flake and its own pinned nixpkgs.
	if err := e.runInChrootBare(os.Stderr, "/bin/sh", installer, "--no-daemon", "--no-channel-add"); err != nil {
		return fmt.Errorf("nix installation failed: %w", err)
	}
	okay(e.nixDir())
	return nil
}

// No profile sourcing: used before nix exists.
func (e *Env) runInChrootBare(stderr *os.File, argv ...string) error {
	args := append([]string{e.nixDir()}, argv...)
	cmd := exec.Command(e.chrootBin(), args...)
	cmd.Env = e.chrootEnv("PATH=/usr/bin:/bin")
	cmd.Stdin = os.Stdin
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
}

func (e *Env) cloneConf(repo string) error {
	if _, err := os.Stat(filepath.Join(e.ConfDir, "flake.nix")); err == nil {
		okay("already cloned at " + e.ConfDir)
		return nil
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git is needed to clone the configuration, and is not on PATH")
	}
	if err := os.MkdirAll(filepath.Dir(e.ConfDir), 0o755); err != nil {
		return err
	}
	cmd := exec.Command(git, "clone", "--depth", "1", repo, e.ConfDir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cloning %s: %w", repo, err)
	}
	okay(e.ConfDir)
	return nil
}
