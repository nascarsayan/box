package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	markerBegin = "# >>> box >>>"
	markerEnd   = "# <<< box <<<"
)

// box is not added to the outer PATH; the aliases reach it by absolute path, so a
// deleted sandbox leaves nothing dangling. ,gwq is a function because it has to cd
// the calling shell.
func (e *Env) rcBlock() string {
	b := e.boxBin()
	return strings.Join([]string{
		fmt.Sprintf("alias ,='%s'", b),
		fmt.Sprintf("alias ,,='%s tmux'", b),
		fmt.Sprintf("alias ,rv='%s review'", b),
		fmt.Sprintf("alias ,gcw='%s gc'", b),
		",gwq() {",
		"  local d",
		fmt.Sprintf("  d=\"$(%s gwq \"$@\")\" || return $?", b),
		"  [ -n \"$d\" ] && cd \"$d\"",
		"}",
	}, "\n")
}

// Only rc files that already exist; creating one uninvited is presumptuous.
func (e *Env) rcCandidates() []string {
	var out []string
	for _, name := range []string{".bashrc", ".zshrc"} {
		p := filepath.Join(e.RealHome, name)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Idempotent: reinstalling must not accumulate copies or leave a stale block.
func upsertBlock(path, body string) (bool, error) {
	want := markerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + markerEnd + "\n"

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	kept, _ := splitOutBlock(string(existing))

	var b strings.Builder
	if len(kept) > 0 {
		b.WriteString(strings.Join(kept, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString(want)

	if string(existing) == b.String() {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(b.String()), 0o644)
}

func stripBlock(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	kept, found := splitOutBlock(string(existing))
	if !found {
		return false, nil
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}

// Trailing blanks dropped, so repeats do not push the block down behind a gap.
func splitOutBlock(content string) ([]string, bool) {
	var kept []string
	inBlock, found := false, false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch strings.TrimSpace(line) {
		case markerBegin:
			inBlock, found = true, true
			continue
		case markerEnd:
			inBlock = false
			continue
		}
		if !inBlock {
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	return kept, found
}
