package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type editorDoneMsg struct{ err error }

// Abre o arquivo na linha indicada no $VISUAL/$EDITOR (vi como padrão).
func openInEditor(root, path string, line int) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	file := filepath.Join(root, path)
	ln := fmt.Sprint(max(line, 1))
	switch filepath.Base(parts[0]) {
	case "code", "cursor", "codium":
		parts = append(parts, "-g", file+":"+ln)
	case "zed", "subl", "hx", "helix":
		parts = append(parts, file+":"+ln)
	default: // vim, nvim, nano, micro, emacs...
		parts = append(parts, "+"+ln, file)
	}
	c := exec.Command(parts[0], parts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

func lastLines(text, fallback string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return fallback
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " | ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func cycle(xs []string, cur string) string {
	for i, x := range xs {
		if x == cur {
			return xs[(i+1)%len(xs)]
		}
	}
	return xs[0]
}

func scrollTo(idx, off, height int) int {
	if height < 1 {
		height = 1
	}
	if idx < off {
		return idx
	}
	if idx >= off+height {
		return idx - height + 1
	}
	return off
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", um)
	}
	return fmt.Sprintf("%d %s", n, varios)
}
