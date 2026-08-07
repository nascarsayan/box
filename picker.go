package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// A hand-rolled bubbletea model rather than huh.MultiSelect, because the detail
// has to follow the *cursor*. huh binds DescriptionFunc to a field's value,
// which for a multi-select is the set of chosen items, not the highlighted one.
//
// Selection is per app. Modules are headings: they group apps and carry the
// summary, but ticking one is just a shortcut for ticking everything under it.
// "kubectl but not k9s" is a normal thing to want.
type row struct {
	module string
	app    *App // nil on a module heading
}

type picker struct {
	rows      []row
	on        map[string]bool // by app name
	cursor    int
	width     int
	done      bool
	cancelled bool
}

// link renders an OSC 8 terminal hyperlink. Terminals that do not understand it
// simply show the label, so there is no fallback to detect.
func link(label, url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func newPicker(meta map[string]ModuleMeta, enabled []string) *picker {
	mods := make([]string, 0, len(meta))
	for n := range meta {
		mods = append(mods, n)
	}
	sort.Strings(mods)

	var rows []row
	for _, m := range mods {
		rows = append(rows, row{module: m})
		apps := meta[m].Apps
		for i := range apps {
			rows = append(rows, row{module: m, app: &apps[i]})
		}
	}

	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}
	p := &picker{rows: rows, on: on, width: 80}
	// Start on the first app, not the heading above it.
	if len(rows) > 1 {
		p.cursor = 1
	}
	return p
}

func (p *picker) Init() tea.Cmd { return nil }

// appsIn returns the app names under a module heading.
func (p *picker) appsIn(module string) []string {
	var out []string
	for _, r := range p.rows {
		if r.app != nil && r.module == module {
			out = append(out, r.app.Name)
		}
	}
	return out
}

// toggle flips the row under the cursor. On a heading it sets every app in the
// group to whatever the group is not already: all-on becomes all-off.
func (p *picker) toggle() {
	r := p.rows[p.cursor]
	if r.app != nil {
		p.on[r.app.Name] = !p.on[r.app.Name]
		return
	}
	apps := p.appsIn(r.module)
	all := true
	for _, a := range apps {
		if !p.on[a] {
			all = false
			break
		}
	}
	for _, a := range apps {
		p.on[a] = !all
	}
}

func (p *picker) setAll(v bool) {
	for _, r := range p.rows {
		if r.app != nil {
			p.on[r.app.Name] = v
		}
	}
}

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		// Guarded: a pty with no size reports 0, which would clamp the wrap width
		// to its floor and break rendering.
		if m.Width > 0 {
			p.width = m.Width
		}
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.rows)-1 {
				p.cursor++
			}
		case " ", "x":
			p.toggle()
		case "a":
			p.setAll(true)
		case "n":
			p.setAll(false)
		case "enter":
			p.done = true
			return p, tea.Quit
		case "q", "esc", "ctrl+c":
			p.cancelled = true
			return p, tea.Quit
		}
	}
	return p, nil
}

// groupMark shows whether a module is fully on, fully off, or mixed. Mixed is
// the state that matters: it is the whole point of per-app selection, and a
// plain checkbox would hide it.
func (p *picker) groupMark(module string) string {
	apps := p.appsIn(module)
	n := 0
	for _, a := range apps {
		if p.on[a] {
			n++
		}
	}
	switch {
	case n == 0:
		return "[ ]"
	case n == len(apps):
		return okStyle.Render("[x]")
	default:
		return okStyle.Render("[~]")
	}
}

func (p *picker) View() string {
	if p.done || p.cancelled {
		return ""
	}
	var b strings.Builder

	b.WriteString(titleStyle.Render("what to install") + "\n")
	b.WriteString(mutedStyle.Render(
		"space toggles (on a group: all of it) · a all · n none · enter confirm · q cancel") + "\n\n")

	for i, r := range p.rows {
		cur := i == p.cursor
		pointer := "  "
		if cur {
			pointer = accentStyle.Render("❯ ")
		}

		if r.app == nil {
			name := r.module
			if cur {
				name = accentStyle.Bold(true).Render(name)
			}
			b.WriteString(pointer + p.groupMark(r.module) + " " + name + "  " +
				mutedStyle.Render(p.meta(r.module)) + "\n")
			continue
		}

		mark := "[ ]"
		if p.on[r.app.Name] {
			mark = okStyle.Render("[x]")
		}
		name := fmt.Sprintf("%-14s", r.app.Name)
		if cur {
			// The link goes on the highlighted row only: an app list where every
			// entry is underlined is harder to read, not easier.
			name = accentStyle.Bold(true).Render(link(r.app.Name, r.app.URL)) +
				strings.Repeat(" ", max(0, 14-len(r.app.Name)))
		}
		b.WriteString("    " + pointer + mark + " " + name + " " +
			mutedStyle.Render(r.app.Size) + "\n")
	}

	b.WriteString("\n" + mutedStyle.Render(p.footer()))
	return b.String()
}

func (p *picker) meta(module string) string {
	apps := p.appsIn(module)
	return fmt.Sprintf("%d apps", len(apps))
}

func (p *picker) footer() string {
	n, total := 0, 0
	for _, r := range p.rows {
		if r.app == nil {
			continue
		}
		total++
		if p.on[r.app.Name] {
			n++
		}
	}
	return fmt.Sprintf("%d of %d selected", n, total)
}

func (p *picker) selected() []string {
	out := []string{}
	for _, r := range p.rows {
		if r.app != nil && p.on[r.app.Name] {
			out = append(out, r.app.Name)
		}
	}
	return out
}

// pickApps runs the picker and returns the chosen app names.
func pickApps(meta map[string]ModuleMeta, enabled []string) ([]string, error) {
	p := newPicker(meta, enabled)
	// Output goes to stderr so that a command whose stdout is data stays pipeable.
	prog := tea.NewProgram(p, tea.WithOutput(os.Stderr), tea.WithInput(os.Stdin))
	if _, err := prog.Run(); err != nil {
		return nil, err
	}
	if p.cancelled {
		return nil, fmt.Errorf("cancelled")
	}
	return p.selected(), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
