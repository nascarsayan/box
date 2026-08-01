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
	"sort"
	"strings"
)

// Set by ldflags at release time.
var version = "dev"

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
		cmdInit, cmdShell, cmdRun, cmdApply, cmdModules, cmdConfig,
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
		{"lifecycle", []string{"init", "apply", "prune", "remove", "self-update"}},
		{"using it", []string{"shell", "run", "tmux"}},
		{"configuration", []string{"modules", "config"}},
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

var cmdVersion = &command{
	name:    "version",
	summary: "print the box version",
	run: func(_ *Env, _ []string) error {
		fmt.Println(version)
		return nil
	},
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
