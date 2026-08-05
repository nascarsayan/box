// box manages a rootless dev sandbox: nix-user-chroot + home-manager, no root.
//
// Static (CGO_ENABLED=0) because the same binary runs both outside the sandbox, to
// create and delete it, and inside it. A binary in the nix store could not delete
// the store it lives in.
//
// The configuration is a separate repo cloned to ~/.config/box; box only applies it.
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

// Set by ldflags at release time. For any other build -- `go build`, `go
// install`, a rebuild from a checkout -- these stay empty and the values come
// from the VCS stamps Go embeds instead, so `box version` still identifies
// exactly what is running.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

type command struct {
	name    string
	args    string
	summary string
	// Shown by `box help <cmd>`.
	long string
	run  func(*Env, []string) error
}

var commands []*command

func init() {
	commands = []*command{
		cmdInit, cmdShell, cmdRun, cmdApply, cmdUpdate, cmdContribute, cmdModules, cmdConfig,
		cmdGwq, cmdReview, cmdGC, cmdTmux,
		cmdDoctor, cmdPrune, cmdRemove, cmdSelfUpdate, cmdVersion,
	}
}

func lookup(name string) *command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return nil
}

func main() {
	args := os.Args[1:]

	// Bare `box` enters the sandbox; that is what `,` runs.
	if len(args) == 0 {
		args = []string{"shell"}
	}

	switch args[0] {
	case "-h", "--help", "help":
		if len(args) > 1 {
			if c := lookup(args[1]); c != nil {
				printCommandHelp(c)
				return
			}
			fatalf("unknown command %q", args[1])
		}
		printHelp()
		return
	case "-v", "--version":
		args[0] = "version"
	}

	name := args[0]
	rest := args[1:]
	c := lookup(name)
	if c == nil {
		if name == "--" {
			c = cmdRun
		} else {
			fatalf("unknown command %q\n\nRun 'box --help' for usage.", name)
		}
	}

	env, err := loadEnv()
	if err != nil {
		fatalf("%v", err)
	}

	if err := c.run(env, rest); err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, errStyle.Render(" error ")+fmt.Sprintf(format, a...))
	os.Exit(1)
}

func printHelp() {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleStyle.Render("box"))
	fmt.Fprintf(&b, "%s\n\n", mutedStyle.Render("a rootless dev sandbox: nix-user-chroot + home-manager, no root required"))
	fmt.Fprintf(&b, "%s\n  box [command] [flags]\n\n", boldStyle.Render("USAGE"))
	fmt.Fprintf(&b, "%s\n", mutedStyle.Render("  With no command, box enters the sandbox shell."))
	fmt.Fprintf(&b, "\n%s\n", boldStyle.Render("COMMANDS"))

	groups := []struct {
		title string
		names []string
	}{
		{"lifecycle", []string{"init", "apply", "update", "prune", "remove", "self-update"}},
		{"using it", []string{"shell", "run", "tmux"}},
		{"configuration", []string{"modules", "config", "contribute"}},
		{"review workflow", []string{"gwq", "review", "gc"}},
		{"other", []string{"doctor", "version"}},
	}
	maxNameLen := 0
	for _, c := range commands {
		if len(c.name) > maxNameLen {
			maxNameLen = len(c.name)
		}
	}

	for _, g := range groups {
		fmt.Fprintf(&b, "\n  %s\n", mutedStyle.Render(g.title))
		for _, n := range g.names {
			c := lookup(n)
			if c == nil {
				continue
			}
			// Name only; arg syntax lives in `box help <cmd>`.
			fmt.Fprintf(&b, "    %s%s %s\n",
				accentStyle.Render(c.name),
				strings.Repeat(" ", maxNameLen-len(c.name)),
				c.summary)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", boldStyle.Render("LEARN MORE"))
	fmt.Fprintf(&b, "  box help <command>   full description, flags and caveats\n")
	fmt.Fprintf(&b, "  box doctor           check this host can run a sandbox\n")
	fmt.Fprint(os.Stdout, b.String())
}

func printCommandHelp(c *command) {
	invocation := "box " + c.name
	if c.args != "" {
		invocation += " " + c.args
	}
	fmt.Printf("%s\n  %s\n\n", boldStyle.Render("USAGE"), accentStyle.Render(invocation))
	fmt.Printf("%s\n  %s\n", boldStyle.Render("SUMMARY"), c.summary)
	if c.long != "" {
		fmt.Printf("\n%s\n", strings.TrimRight(indent(c.long, "  "), "\n"))
	}
	fmt.Println()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimLeft(s, "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// vcsStamps reads the commit, build time and dirty flag that Go embeds when
// building from a git checkout. Release builds pass these via ldflags and take
// precedence; this is what makes a locally built binary identifiable.
func vcsStamps() (rev, when string, dirty bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return rev, when, dirty
}

var cmdVersion = &command{
	name:    "version",
	args:    "[--short]",
	summary: "print the box version",
	long: `Reports the running binary and the configuration it is driving.

Two things determine box's behaviour: the binary and the config clone. A version
that named only the binary would not tell you what actually ran, so both are
printed, each with its git revision.

  box version            everything
  box version --short    just the binary version, for scripts`,
	run: func(e *Env, args []string) error {
		rev, when, dirty := vcsStamps()
		if commit == "" {
			commit = rev
		}
		if date == "" {
			date = when
		}

		v := version
		// An untagged build says "dev"; pin it to a commit so two dev builds
		// are still tellable apart.
		if v == "dev" && commit != "" {
			v = "dev+" + short(commit)
		}
		if dirty {
			v += " (dirty)"
		}

		for _, a := range args {
			if a == "--short" {
				fmt.Println(v)
				return nil
			}
		}

		fmt.Printf("box      %s\n", v)
		if commit != "" {
			fmt.Printf("commit   %s\n", commit)
		}
		if date != "" {
			fmt.Printf("built    %s\n", date)
		}
		fmt.Printf("go       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

		// The config clone is the other half of "which version is running".
		if e != nil && e.ConfDir != "" {
			fmt.Printf("config   %s\n", e.ConfDir)
			if out, err := e.git("rev-parse", "--short", "HEAD"); err == nil && out != "" {
				st, _ := e.git("status", "--porcelain")
				mark := ""
				if st != "" {
					mark = " (dirty)"
				}
				desc, _ := e.git("describe", "--tags", "--always")
				fmt.Printf("config@  %s%s%s\n", out, dashPrefix(desc, out), mark)
			}
		}
		return nil
	},
}

// dashPrefix renders " (tag)" when describe says something beyond the bare sha.
func dashPrefix(desc, sha string) string {
	if desc == "" || desc == sha {
		return ""
	}
	return " " + desc
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
