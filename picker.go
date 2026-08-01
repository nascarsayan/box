package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// A hand-rolled bubbletea model rather than huh.MultiSelect, because the detail
// pane has to follow the *cursor*. huh binds DescriptionFunc to a field's value,
// which for a multi-select is the set of chosen items, not the highlighted one --
// so the description could not track what you are looking at.
type picker struct {
	names     []string
	meta      map[string]ModuleMeta
	on        map[string]bool
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
	names := make([]string, 0, len(meta))
	for n := range meta {
		names = append(names, n)
	}
	sort.Strings(names)

	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}
	return &picker{names: names, meta: meta, on: on, width: 80}
}

func (p *picker) Init() tea.Cmd { return nil }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		// Guarded: a pty with no size reports 0, which would clamp the wrap width
		// to its floor and break the app list after the first entry.
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
			if p.cursor < len(p.names)-1 {
				p.cursor++
			}
		case " ", "x":
			n := p.names[p.cursor]
			p.on[n] = !p.on[n]
		case "a":
			for _, n := range p.names {
				p.on[n] = true
			}
		case "n":
			for _, n := range p.names {
				p.on[n] = false
			}
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

func (p *picker) View() string {
	if p.done || p.cancelled {
		return ""
	}
	var b strings.Builder

	b.WriteString(titleStyle.Render("modules") + "\n")
	b.WriteString(mutedStyle.Render("space toggles · a all · n none · enter confirm · q cancel") + "\n\n")

	for i, n := range p.names {
		m := p.meta[n]

		mark := "[ ]"
		if p.on[n] {
			mark = okStyle.Render("[x]")
		}
		name := fmt.Sprintf("%-13s", n)
		if i == p.cursor {
			b.WriteString(accentStyle.Render("❯ ") + mark + " " +
				accentStyle.Bold(true).Render(name) + " " + mutedStyle.Render(m.Size) + "\n")
			// Detail for the highlighted row only. That is the point: the list stays
			// scannable, and the apps appear where you are already looking.
			b.WriteString(p.detail(m))
		} else {
			b.WriteString("  " + mark + " " + name + " " + mutedStyle.Render(m.Size) + "\n")
		}
	}

	b.WriteString("\n" + mutedStyle.Render(p.footer()))
	return b.String()
}

func (p *picker) detail(m ModuleMeta) string {
	indent := "    "
	var b strings.Builder
	b.WriteString(indent + mutedStyle.Render(m.Summary) + "\n")

	// App names as clickable links, wrapped to the terminal width. Rendered with
	// the raw escape rather than through lipgloss, whose width accounting would
	// count the OSC 8 bytes as visible characters.
	wrap := p.width - len(indent) - 4
	if wrap < 20 {
		wrap = 20
	}
	line, plain := indent, len(indent)
	for i, a := range m.Apps {
		if plain+len(a.Name)+2 > wrap && i > 0 {
			b.WriteString(line + "\n")
			line, plain = indent, len(indent)
		}
		line += link(a.Name, a.URL) + "  "
		plain += len(a.Name) + 2
	}
	b.WriteString(strings.TrimRight(line, " ") + "\n\n")
	return b.String()
}

func (p *picker) footer() string {
	n := 0
	for _, v := range p.on {
		if v {
			n++
		}
	}
	return fmt.Sprintf("%d of %d selected", n, len(p.names))
}

func (p *picker) selected() []string {
	out := []string{}
	for _, n := range p.names {
		if p.on[n] {
			out = append(out, n)
		}
	}
	return out
}

// pickModules runs the picker and returns the chosen set.
func pickModules(meta map[string]ModuleMeta, enabled []string) ([]string, error) {
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
