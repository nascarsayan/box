package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Stripped from the inherited environment. The rest is inherited on purpose
// (DISPLAY, SSH_AUTH_SOCK, VSCODE_*, proxies, TERM), but another sandbox exports
// vars pointing into itself: an inherited ZDOTDIR sends this zsh to read its
// .zshrc, and __HM_SESS_VARS_SOURCED=1 makes hm-session-vars.sh export nothing.
var strippedVars = []string{
	"ZDOTDIR",
	"ZINIT_HOME",
	"P10K_CONFIG",
	"FZF_HOME",
	"HELIX_CONFIG_DIR",
	"HELIX_RUNTIME",
	"ZSH",
	"ZSH_CACHE_DIR",
	"ZSH_COMPDUMP",
	"NIX_PATH",
	"NIX_PROFILES",
	"NIX_SSL_CERT_FILE",
	"__HM_SESS_VARS_SOURCED",
}

// chrootEnv builds the environment for a process inside the namespace.
func (e *Env) chrootEnv(extra ...string) []string {
	strip := map[string]bool{}
	for _, k := range strippedVars {
		strip[k] = true
	}
	// These are set explicitly below; drop any inherited value first.
	for _, k := range []string{"HOME", "SANDBOX_HOME", "TMPDIR", "TMUX_TMPDIR", "BOX_INSIDE", "BOX_REAL_HOME", "BOX_OUTER_PATH"} {
		strip[k] = true
	}

	var out []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && strip[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}

	out = append(out,
		"HOME="+e.SandboxDir,
		// Matches core/home.nix's programs.zsh.dotDir.
		"ZDOTDIR="+e.SandboxDir,
		"SANDBOX_HOME="+e.SandboxDir,
		"BOX_REAL_HOME="+e.RealHome,
		"BOX_CONF_DIR="+e.ConfDir,
		"BOX_SANDBOX_DIR="+e.SandboxDir,
		// So a nested `box` knows not to re-enter the chroot.
		"BOX_INSIDE=1",
		// /tmp is small and shared; nix builds fill it.
		"TMPDIR="+e.tmpDir(),
		// Real path, so a tmux server in one namespace is reachable from another.
		"TMUX_TMPDIR="+filepath.Join(e.SandboxDir, "run"),
		// Appended, not replaced: host-only tools (VS Code's `code`) must resolve.
		"BOX_OUTER_PATH="+os.Getenv("PATH"),
	)
	if ca := caBundle(); ca != "" {
		out = append(out, "NIX_SSL_CERT_FILE="+ca)
	}
	return append(out, extra...)
}

// sh rather than Go because the profile scripts have to be *sourced*.
const innerScript = `
set -e
if [ -r "$HOME/.nix-profile/etc/profile.d/nix.sh" ]; then
  . "$HOME/.nix-profile/etc/profile.d/nix.sh"
fi
if [ -r "$HOME/.nix-profile/etc/profile.d/hm-session-vars.sh" ]; then
  . "$HOME/.nix-profile/etc/profile.d/hm-session-vars.sh"
fi
# The generated .zshrc builds fpath by walking $NIX_PROFILES. Unset, every
# completion shipped by a nix package is silently invisible -- gh <TAB> does
# nothing and nothing says why. Set it rather than trust nix.sh to have run.
if [ -z "${NIX_PROFILES:-}" ]; then
  NIX_PROFILES="/nix/var/nix/profiles/default $HOME/.nix-profile"
  export NIX_PROFILES
fi
PATH="$HOME/.nix-profile/bin:$PATH:$BOX_OUTER_PATH"
export PATH
exec "$@"
`

// inChroot runs argv inside the namespace, replacing this process.
func (e *Env) inChroot(argv ...string) error {
	if err := e.requireSandbox(); err != nil {
		return err
	}
	_ = os.MkdirAll(e.tmpDir(), 0o755)
	_ = os.MkdirAll(filepath.Join(e.SandboxDir, "run"), 0o755)

	full := append([]string{
		e.chrootBin(), e.nixDir(),
		"/bin/sh", "-c", innerScript, "_",
	}, argv...)

	return syscall.Exec(e.chrootBin(), full[0:], e.chrootEnv())
}

// runInChroot runs argv inside the namespace and waits, streaming output.
func (e *Env) runInChroot(stdout, stderr *os.File, argv ...string) error {
	if err := e.requireSandbox(); err != nil {
		return err
	}
	_ = os.MkdirAll(e.tmpDir(), 0o755)
	_ = os.MkdirAll(filepath.Join(e.SandboxDir, "run"), 0o755)

	args := append([]string{e.nixDir(), "/bin/sh", "-c", innerScript, "_"}, argv...)
	cmd := exec.Command(e.chrootBin(), args...)
	cmd.Env = e.chrootEnv()
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// capture runs argv inside the namespace and returns stdout.
func (e *Env) capture(argv ...string) (string, error) {
	if err := e.requireSandbox(); err != nil {
		return "", err
	}
	args := append([]string{e.nixDir(), "/bin/sh", "-c", innerScript, "_"}, argv...)
	cmd := exec.Command(e.chrootBin(), args...)
	cmd.Env = e.chrootEnv()
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Runs directly when already inside the sandbox, else re-enters it -- so helpers
// behave identically from a login bash and from the sandbox zsh.
func (e *Env) dispatch(argv ...string) error {
	if e.InsideSandbox {
		bin, err := e.lookPathIn(argv[0])
		if err != nil {
			return err
		}
		return syscall.Exec(bin, argv, os.Environ())
	}
	return e.inChroot(argv...)
}

// For commands whose stdout is data, not display.
func (e *Env) dispatchCapture(argv ...string) (string, error) {
	if e.InsideSandbox {
		bin, err := e.lookPathIn(argv[0])
		if err != nil {
			return "", err
		}
		cmd := exec.Command(bin, argv[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	return e.capture(argv...)
}

var cmdShell = &command{
	name:    "shell",
	summary: "enter the sandbox (this is what bare `box` and `,` do)",
	long: `Starts an interactive login zsh inside the sandbox.

nix-user-chroot bind-mounts <sandbox>/nix onto /nix in a fresh mount and user
namespace. Nothing else is remapped, so NFS mounts and your real home stay visible
at their usual paths -- but /nix does not exist outside the namespace, which is why
everything the sandbox provides has to be reached through box.

$HOME inside is the sandbox directory. That is what lets the configuration be
ordinary home-manager output (~/.zshrc, ~/.tmux.conf) rather than a pile of
ZDOTDIR/XDG overrides. Selected paths -- ws, .ssh, .kube and so on -- are
symlinked back to the real home; see 'box help config'.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		if len(rest) > 0 {
			return e.inChroot(rest...)
		}
		if e.InsideSandbox {
			return fmt.Errorf("already inside the sandbox")
		}
		return e.inChroot("zsh", "-l")
	},
}

var cmdRun = &command{
	name:    "run",
	args:    "-- CMD [ARGS...]",
	summary: "run one command inside the sandbox",
	long: `Runs a single command inside the sandbox and exits with its status.

  box run -- kubectl get pods
  box run -- zsh -ic 'echo $GITTOP'

The environment matches an interactive sandbox shell: the nix profile is on PATH
first, the host PATH after it, and home-manager's session variables are exported.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return fmt.Errorf("nothing to run\n       usage: box run -- CMD [ARGS...]")
		}
		return e.inChroot(rest...)
	},
}
