package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

// Referências numeradas: cada arquivo:linha citado pelo modelo (num cartão ou no texto)
// ganha um número [n]. ctrl+g abre a lista; o número ou enter mostra o trecho no
// Visualizador: no diff, se o arquivo mudou no alvo; senão, o arquivo na aba Documento.

type refEntry struct {
	path string
	line int
}

// addRef devolve o número da referência, criando-a se for nova.
func (m *model) addRef(p string, line int) int {
	for i, r := range m.refs {
		if r.path == p && r.line == line {
			return i + 1
		}
	}
	m.refs = append(m.refs, refEntry{path: p, line: line})
	return len(m.refs)
}

// resolveRefPath confirma que o caminho citado existe no repositório. Um nome sem pasta
// ("openai.go") vale se só um arquivo alterado do alvo tiver esse nome.
func (m model) resolveRefPath(p string) (string, bool) {
	p = strings.TrimPrefix(strings.Trim(p, "`*\"'"), "./")
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") || m.repo.Root == "" {
		return "", false
	}
	for _, f := range m.files {
		if f.Path == p {
			return p, true
		}
	}
	if !strings.Contains(p, "/") {
		found := ""
		for _, f := range m.files {
			if path.Base(f.Path) == p {
				if found != "" {
					return "", false // ambíguo
				}
				found = f.Path
			}
		}
		if found != "" {
			return found, true
		}
	}
	if st, err := os.Stat(filepath.Join(m.repo.Root, filepath.FromSlash(p))); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

var refRe = regexp.MustCompile("([A-Za-z0-9_.\\-/]+\\.[A-Za-z0-9]+):(\\d+)(?:-\\d+)?")

// annotateRefs acrescenta [n] depois de cada arquivo:linha válido no texto (fora de
// blocos de código).
func (m *model) annotateRefs(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = refRe.ReplaceAllStringFunc(l, func(match string) string {
			sm := refRe.FindStringSubmatch(match)
			p, ok := m.resolveRefPath(sm[1])
			if !ok {
				return match
			}
			n, _ := strconv.Atoi(sm[2])
			return fmt.Sprintf("%s [%d]", match, m.addRef(p, n))
		})
	}
	return strings.Join(lines, "\n")
}

// ---- abrir uma referência ----

type refDocMsg struct {
	key, title, content string
	line                int
	err                 error
}

// openRef mostra a referência n no Visualizador.
func (m model) openRef(n int) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	if n < 1 || n > len(m.refs) {
		m.setInfo(fmt.Sprintf("não há referência [%d]", n))
		return m, nil
	}
	r := m.refs[n-1]
	if m.lay.isMinimized(pDiff) {
		m.lay.restore(pDiff)
		m.layoutChanged()
	}
	m.setInfo(fmt.Sprintf("[%d] %s:%d", n, r.path, r.line))
	// Arquivo alterado no alvo: o diff, rolado até a linha.
	for i, f := range m.files {
		if f.Path == r.path {
			m.fileIdx, m.navOpen = i, true
			m.fixFilesScroll()
			m.pendingLine = r.line
			if m.diffPath == r.path && m.diffRaw != "" && m.previewKey == m.target+"\x00"+r.path {
				m.setDiff(m.diffPath, m.diffRaw) // já carregado
				return m, nil
			}
			return m, m.previewSelected()
		}
	}
	// Outro arquivo: lido como o modelo leria (mesmas proteções), na versão do alvo.
	m.pendingLine = r.line
	key := fmt.Sprintf("ref\x00%s\x00%d", r.path, r.line)
	m.previewKey = key
	exec := m.repo.Executor(func() string { return m.target })
	args := map[string]any{"path": r.path, "inteiro": true}
	if m.target != repo.WorkTree {
		args["ref"] = repo.Tip(m.target)
	}
	b, _ := json.Marshal(args)
	title := fmt.Sprintf("%s:%d · referência [%d]", r.path, r.line, n)
	return m, func() tea.Msg {
		out, isErr := exec(context.Background(), llm.ToolCall{Name: "read_file", Args: string(b)})
		if isErr && args["ref"] != nil {
			// O arquivo não existe naquele commit: mostra a versão atual, avisando.
			cur, _ := json.Marshal(map[string]any{"path": r.path, "inteiro": true})
			if out2, bad := exec(context.Background(), llm.ToolCall{Name: "read_file", Args: string(cur)}); !bad {
				return refDocMsg{key: key, title: title + " · versão atual (não existe neste commit)", content: out2, line: r.line}
			}
		}
		if isErr {
			return refDocMsg{key: key, err: fmt.Errorf("%s", out)}
		}
		return refDocMsg{key: key, title: title, content: out, line: r.line}
	}
}

func (m model) onRefDoc(msg refDocMsg) (tea.Model, tea.Cmd) {
	if msg.key != m.previewKey {
		return m, nil
	}
	if msg.err != nil {
		m.pendingLine = 0
		m.setErr("não consegui abrir a referência: " + msg.err.Error())
		return m, nil
	}
	m.pendingLine = msg.line
	m.setDoc(msg.title, msg.content, false)
	return m, nil
}

// ---- lista de referências (ctrl+g) ----

func (m model) updateRefKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+g", "q":
		m.mode = modeNormal
	case "up", "k":
		m.refIdx = max(m.refIdx-1, 0)
	case "down", "j":
		m.refIdx = min(m.refIdx+1, max(len(m.refs)-1, 0))
	case "enter":
		return m.openRef(m.refIdx + 1)
	default:
		if n, err := strconv.Atoi(key); err == nil {
			// Até 9 referências, um dígito abre; acima disso, dois dígitos seguidos.
			if m.refDigits != "" {
				n, _ = strconv.Atoi(m.refDigits + key)
				m.refDigits = ""
				return m.openRef(n)
			}
			if len(m.refs) >= 10 && n*10 <= len(m.refs) {
				m.refDigits = key
				return m, nil
			}
			return m.openRef(n)
		}
	}
	return m, nil
}

func (m model) refsPopup() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("referências") + "\n\n")
	if len(m.refs) == 0 {
		b.WriteString(subtleStyle.Render("Nenhuma ainda. Elas aparecem como [1], [2]…\nquando o modelo cita arquivo:linha.") + "\n")
	}
	start := max(0, min(m.refIdx-12, len(m.refs)-14))
	for i := start; i < len(m.refs) && i < start+14; i++ {
		r := m.refs[i]
		line := fmt.Sprintf("[%d] %s:%d", i+1, r.path, r.line)
		if i == m.refIdx {
			b.WriteString(selectedStyle.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + refStyle.UnsetBold().Render(line) + "\n")
		}
	}
	b.WriteString("\n" + subtleStyle.Render("número ou enter abre no Visualizador · esc fecha"))
	return boxStyle.Padding(1, 2).Render(b.String())
}
