package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// Env is where everything lives on this machine. Resolution order: flag, $BOX_*,
// state file, XDG default. The state file is what lets `box init -c elsewhere` stick
// without repeating the flag.
type Env struct {
	// ConfDir is the local clone of the configuration repo.
	ConfDir string
	// SandboxDir holds the nix store, the chroot binary, and $HOME inside.
	SandboxDir string
	// RealHome is the actual account home, outside the sandbox.
	RealHome string
	Username string

	// InsideSandbox is true when we are already within the chroot, in which case
	// commands run directly instead of re-entering it.
	InsideSandbox bool
}

const (
	envConfDir    = "BOX_CONF_DIR"
	envSandboxDir = "BOX_SANDBOX_DIR"
	stateFileName = "state.json"
)

type state struct {
	ConfDir    string `json:"confDir"`
	SandboxDir string `json:"sandboxDir"`
}

// Separate from ConfDir, which is a git clone box may replace wholesale.
func stateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "box")
	}
	return filepath.Join(realHomeGuess(), ".local", "state", "box")
}

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(realHomeGuess(), ".config")
}

// realHomeGuess resolves the *outer* home. $HOME is unreliable: inside the sandbox
// it is the sandbox. passwd still reports the real one, which is why it beats $HOME.
func realHomeGuess() string {
	if v := os.Getenv("BOX_REAL_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("SANDBOX_REAL_HOME"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return os.Getenv("HOME")
}

func loadEnv() (*Env, error) {
	e := &Env{
		RealHome:      realHomeGuess(),
		InsideSandbox: os.Getenv("BOX_INSIDE") == "1",
	}
	if u, err := user.Current(); err == nil {
		e.Username = u.Username
	}

	var st state
	if b, err := os.ReadFile(filepath.Join(stateHome(), stateFileName)); err == nil {
		_ = json.Unmarshal(b, &st)
	}

	e.ConfDir = firstNonEmpty(
		os.Getenv(envConfDir),
		st.ConfDir,
		filepath.Join(xdgConfigHome(), "box"),
	)
	e.SandboxDir = firstNonEmpty(
		os.Getenv(envSandboxDir),
		st.SandboxDir,
		filepath.Join(e.RealHome, "sandbox"),
	)
	return e, nil
}

func (e *Env) saveState() error {
	if err := os.MkdirAll(stateHome(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state{ConfDir: e.ConfDir, SandboxDir: e.SandboxDir}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateHome(), stateFileName), append(b, '\n'), 0o644)
}

func (e *Env) nixDir() string     { return filepath.Join(e.SandboxDir, "nix") }
func (e *Env) chrootBin() string  { return filepath.Join(e.SandboxDir, "bin", "nix-user-chroot") }
func (e *Env) boxBin() string     { return filepath.Join(e.SandboxDir, "bin", "box") }
func (e *Env) tmpDir() string     { return filepath.Join(e.SandboxDir, "tmp") }
func (e *Env) profileBin() string { return filepath.Join(e.SandboxDir, ".nix-profile", "bin") }

// Always a path, never github:. Only path: flakes see untracked files, and
// local.json is gitignored -- through a git ref the defaults would silently win.
func (e *Env) flakeRef() string { return "path:" + e.ConfDir }

func (e *Env) requireSandbox() error {
	if _, err := os.Stat(e.chrootBin()); err != nil {
		return fmt.Errorf("no sandbox at %s\n       run 'box init' first", e.SandboxDir)
	}
	if _, err := os.Stat(filepath.Join(e.nixDir(), "store")); err != nil {
		return fmt.Errorf("no nix store at %s\n       run 'box init' first", e.nixDir())
	}
	return nil
}

func (e *Env) requireConf() error {
	if _, err := os.Stat(filepath.Join(e.ConfDir, "flake.nix")); err != nil {
		return fmt.Errorf("no configuration at %s\n       run 'box init' first, or pass -c <dir>", e.ConfDir)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// CA bundle for nix inside the namespace.
func caBundle() string {
	for _, f := range []string{
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/ssl/ca-bundle.pem",
		"/etc/ssl/cert.pem",
	} {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

// lookPathIn finds a binary in the sandbox profile, for use inside the chroot.
func (e *Env) lookPathIn(name string) (string, error) {
	p := filepath.Join(e.profileBin(), name)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found in the sandbox profile", name)
}

// Hand-rolled so unknown args pass through (box run -- cmd --flag).
func (e *Env) parseCommonFlags(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		// Everything after `--` passes through verbatim. Without this,
		// `box run -- sh -c 'script'` loses the script: -c is eaten as --conf-dir.
		if a == "--" {
			rest = append(rest, args[i:]...)
			return rest, nil
		}
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "-c" || a == "--conf-dir":
			v, err := next()
			if err != nil {
				return nil, err
			}
			e.ConfDir = abs(v)
		case strings.HasPrefix(a, "--conf-dir="):
			e.ConfDir = abs(strings.TrimPrefix(a, "--conf-dir="))
		case a == "-d" || a == "--sandbox-dir":
			v, err := next()
			if err != nil {
				return nil, err
			}
			e.SandboxDir = abs(v)
		case strings.HasPrefix(a, "--sandbox-dir="):
			e.SandboxDir = abs(strings.TrimPrefix(a, "--sandbox-dir="))
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

func abs(p string) string {
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(realHomeGuess(), p[2:])
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
