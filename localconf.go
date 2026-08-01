package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// <confDir>/local.json: this machine's answers. JSON so box can rewrite it without
// generating nix; an optional local.nix is merged on top. Both gitignored, which is
// what keeps the tracked tree a clean library.
type LocalConf struct {
	System      string            `json:"system"`
	Username    string            `json:"username"`
	RealHome    string            `json:"realHome"`
	SandboxHome string            `json:"sandboxHome"`
	ConfDir     string            `json:"confDir"`
	GhqRoot     string            `json:"ghqRoot"`
	GhConfigDir string            `json:"ghConfigDir"`
	TmuxSocket  string            `json:"tmuxSocket"`
	Git         GitIdentity       `json:"git"`
	Passthrough []string          `json:"passthrough"`
	Modules     []string          `json:"modules"`
	SessionVars map[string]string `json:"sessionVariables,omitempty"`
}

type GitIdentity struct {
	UserName  string `json:"userName"`
	UserEmail string `json:"userEmail"`
}

func (e *Env) localConfPath() string { return filepath.Join(e.ConfDir, "local.json") }

func (e *Env) readLocalConf() (*LocalConf, error) {
	b, err := os.ReadFile(e.localConfPath())
	if err != nil {
		return nil, err
	}
	var c LocalConf
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", e.localConfPath(), err)
	}
	return &c, nil
}

func (e *Env) writeLocalConf(c *LocalConf) error {
	// A nil slice marshals to JSON null, not [], and nix then fails deep inside
	// lib.subtractLists with "expected a list but found null". `--modules ""` is the
	// obvious way to get here.
	if c.Modules == nil {
		c.Modules = []string{}
	}
	if c.Passthrough == nil {
		c.Passthrough = []string{}
	}
	sort.Strings(c.Modules)
	sort.Strings(c.Passthrough)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.ConfDir, 0o755); err != nil {
		return err
	}
	// Rename: a half-written file makes the whole flake fail to evaluate.
	tmp := e.localConfPath() + ".new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, e.localConfPath())
}

// Everything derivable, so --yes needs no prompts.
func (e *Env) defaultLocalConf(modules []string) *LocalConf {
	name, email := e.gitIdentity()
	return &LocalConf{
		System:      "x86_64-linux",
		Username:    e.Username,
		RealHome:    e.RealHome,
		SandboxHome: e.SandboxDir,
		ConfDir:     e.ConfDir,
		GhqRoot:     e.detectGhqRoot(),
		GhConfigDir: e.detectGhConfigDir(),
		TmuxSocket:  e.Username,
		Git:         GitIdentity{UserName: name, UserEmail: email},
		Passthrough: e.detectPassthrough(),
		Modules:     modules,
	}
}

// `git config --get`, not INI parsing, so include/includeIf resolve as git would.
func (e *Env) gitIdentity() (name, email string) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", ""
	}
	read := func(key string) string {
		cmd := exec.Command(git, "config", "--get", key)
		// HOME may be the sandbox; point git at the real account.
		cmd.Env = append(os.Environ(), "HOME="+e.RealHome)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return read("user.name"), read("user.email")
}

// Reuse an existing ghq.root rather than making a second tree beside it.
func (e *Env) detectGhqRoot() string {
	if git, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command(git, "config", "--get", "ghq.root")
		cmd.Env = append(os.Environ(), "HOME="+e.RealHome)
		if out, err := cmd.Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v
			}
		}
	}
	return filepath.Join(e.RealHome, "ghq")
}

// Prefer a dir that already holds a token, so `gh auth login` is not repeated.
func (e *Env) detectGhConfigDir() string {
	candidates := []string{
		filepath.Join(e.RealHome, ".config", "gh"),
		filepath.Join(e.RealHome, "ws", "dev", ".config", "gh"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "hosts.yml")); err == nil {
			return c
		}
	}
	return candidates[0]
}

// Symlinked back to the real home when present.
var candidatePassthrough = []string{"ws", ".ssh", ".kube", ".claude", ".config/gh", ".azure", ".gnupg"}

// Resolved here, not with builtins.pathExists, so nix eval stays pure.
func (e *Env) detectPassthrough() []string {
	var out []string
	for _, p := range candidatePassthrough {
		if p == ".config/gh" {
			continue // handled separately via ghConfigDir
		}
		if _, err := os.Lstat(filepath.Join(e.RealHome, p)); err == nil {
			out = append(out, p)
		}
	}
	return out
}

var cmdConfig = &command{
	name:    "config",
	summary: "show this machine's resolved configuration",
	long: `Prints <confDir>/local.json, which is what box init and box modules write.

It is gitignored, so the tracked configuration tree stays a clean library: any
change you make to a module shows up as an ordinary git diff and can be sent
upstream, while your own paths and module selection never appear in it.

Edit it by hand if you like -- box only rewrites the fields it owns. For anything
that needs a real nix expression rather than a literal, put a local.nix beside it;
flake.nix merges that on top.`,
	run: func(e *Env, args []string) error {
		if _, err := e.parseCommonFlags(args); err != nil {
			return err
		}
		b, err := os.ReadFile(e.localConfPath())
		if err != nil {
			return fmt.Errorf("no local configuration at %s\n       run 'box init' first", e.localConfPath())
		}
		fmt.Fprintf(os.Stderr, "%s\n", mutedStyle.Render("# "+e.localConfPath()))
		os.Stdout.Write(b)
		return nil
	},
}
