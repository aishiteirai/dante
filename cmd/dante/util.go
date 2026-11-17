package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type editorDoneMsg struct{ err error }

// Abre o arquivo na linha indicada no $VISUAL/$EDITOR (vi no Linux e no macOS, Bloco de
// Notas no Windows).
func openInEditor(root, path string, line int) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	parts := editorCommand(editor, runtime.GOOS, filepath.Join(root, filepath.FromSlash(path)), line)
	c := exec.Command(parts[0], parts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// editorCommand monta a linha de comando que abre `file` na linha `line`.
func editorCommand(editor, goos, file string, line int) []string {
	if editor == "" {
		editor = "vi"
		if goos == "windows" {
			editor = "notepad"
		}
	}
	parts := strings.Fields(editor)
	ln := fmt.Sprint(max(line, 1))
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(parts[0])), ".exe")
	switch name {
	case "code", "cursor", "codium":
		return append(parts, "-g", file+":"+ln)
	case "zed", "subl", "hx", "helix":
		return append(parts, file+":"+ln)
	case "notepad", "notepad++", "write":
		return append(parts, file) // não aceitam a linha na linha de comando
	}
	return append(parts, "+"+ln, file) // vim, nvim, nano, micro, emacs...
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
