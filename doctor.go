package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The one hard requirement, with no fallback: user namespaces are what let an
// unprivileged account have a /nix at all.
func userNSAvailable() (bool, string) {
	if b, err := os.ReadFile("/proc/sys/user/max_user_namespaces"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n < 1 {
			return false, "user.max_user_namespaces is 0; an admin can set it with:\n         sysctl -w user.max_user_namespaces=15000"
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		if strings.TrimSpace(string(b)) == "0" {
			return false, "kernel.unprivileged_userns_clone is 0; an admin must enable it"
		}
	}
	// Both knobs can look fine while a policy still refuses the clone.
	if unshare, err := exec.LookPath("unshare"); err == nil {
		if err := exec.Command(unshare, "--user", "--map-root-user", "true").Run(); err != nil {
			return false, "unshare --user failed; user namespaces are blocked by policy"
		}
	}
	return true, ""
}

var cmdDoctor = &command{
	name:    "doctor",
	summary: "check this host can run a sandbox",
	long: `Verifies the prerequisites and reports on an existing sandbox.

Run it before opening an issue: it distinguishes the three things that actually go
wrong -- user namespaces disabled, no nix store yet, or a config directory box
cannot find.`,
	run: func(e *Env, args []string) error {
		if _, err := e.parseCommonFlags(args); err != nil {
			return err
		}
		fail := 0
		check := func(name string, ok bool, detail string) {
			if ok {
				okay(fmt.Sprintf("%-22s %s", name, detail))
			} else {
				fail++
				warn(fmt.Sprintf("%-22s %s", name, detail))
			}
		}

		ok, why := userNSAvailable()
		if ok {
			check("user namespaces", true, "available")
		} else {
			check("user namespaces", false, why)
		}

		for _, c := range []string{"curl", "git"} {
			p, err := exec.LookPath(c)
			check(c, err == nil, firstNonEmpty(p, "not found on PATH"))
		}

		_, cerr := os.Stat(filepath.Join(e.ConfDir, "flake.nix"))
		check("configuration", cerr == nil, firstNonEmpty(e.ConfDir, "-"))

		_, serr := os.Stat(filepath.Join(e.nixDir(), "store"))
		check("nix store", serr == nil, e.nixDir())

		_, berr := os.Stat(e.chrootBin())
		check("nix-user-chroot", berr == nil, e.chrootBin())

		if serr == nil && berr == nil {
			v, err := e.capture("nix", "--version")
			check("nix", err == nil, firstNonEmpty(v, "not runnable inside the sandbox"))
		}

		if _, err := os.Stat(e.localConfPath()); err == nil {
			conf, err := e.readLocalConf()
			if err == nil {
				check("modules", true, strings.Join(conf.Modules, ", "))
			}
		}

		if fail > 0 {
			return fmt.Errorf("%d check(s) failed", fail)
		}
		fmt.Fprintln(os.Stderr, okStyle.Render("  ok  ")+"ready")
		return nil
	},
}
