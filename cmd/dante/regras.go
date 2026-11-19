package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/mapa"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// /regras (e dante regras): o rascunho do CLAUDE.md (ou AGENTS.md) do projeto, para os
// agentes de código seguirem a estrutura, os comandos e as convenções dele. O dante junta os
// fatos sem o modelo (pastas, tecnologias, comandos do Makefile e do package.json, linters,
// estilo dos commits, README, mapa e o arquivo atual) e pede só a redação.

const regrasSystemPrompt = `Você escreve o arquivo de instruções que agentes de código (Claude Code, Codex, Cursor) leem
antes de trabalhar num repositório. Recebe fatos do projeto. Escreva só o conteúdo do arquivo, em markdown, no idioma
do projeto (pelo README e pelos commits), com no máximo ~120 linhas, nestas seções:
# <nome do projeto>
## Visão geral (2 a 4 frases: o que é e para quem)
## Estrutura (as pastas que importam e o papel de cada uma)
## Comandos (instalar, rodar, testar, lint; só os que existem nos fatos, em blocos de código)
## Convenções (estilo de código e de commits, como mostram os fatos)
## Cuidados (o que não fazer: arquivos gerados, segredos, migrações, o que quebra fácil)
## Antes de terminar uma tarefa (os comandos de teste e lint a rodar)
Seja específico ao projeto e imperativo; não invente comandos nem regras que os fatos não mostram. Se houver um
arquivo de instruções atual, mantenha o que ele tem de útil e escrito à mão, atualizando o resto.`

var (
	makeTarget = regexp.MustCompile(`(?m)^([A-Za-z0-9][A-Za-z0-9_.-]*):(?:[^=]|$)`)
	fenceWrap  = regexp.MustCompile("(?s)^```(?:markdown|md)?\\s*\\n(.*?)\\n```\\s*$")
)

// regrasFacts junta os fatos do projeto para o arquivo de instruções.
func regrasFacts(r repo.Repo, base string) string {
	read := func(p string, n int) string {
		b, err := os.ReadFile(filepath.Join(r.Root, p))
		if err != nil {
			return ""
		}
		s, _ := repo.Redact(string(b))
		if len(s) > n {
			s = s[:n] + "\n…"
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Projeto: %s\n\n", repoDisplayName(r.Root))
	files, _ := r.ListFiles()
	b.WriteString("<pastas>\n" + folderList(files, 60) + "\n</pastas>\n\n")
	for _, man := range []string{"go.mod", "package.json", "pyproject.toml", "requirements.txt", "Cargo.toml", "composer.json", "Gemfile", "pom.xml", "build.gradle"} {
		if s := read(man, 2500); s != "" {
			if man == "package.json" {
				s = summarizePackageJSON(s)
			}
			fmt.Fprintf(&b, "<%s>\n%s\n</%s>\n\n", man, strings.TrimSpace(s), man)
		}
	}
	if mk := read("Makefile", 20000); mk != "" {
		var ts []string
		for _, m := range makeTarget.FindAllStringSubmatch(mk, -1) {
			if !strings.HasPrefix(m[1], ".") {
				ts = append(ts, m[1])
			}
		}
		if len(ts) > 0 {
			b.WriteString("<makefile_alvos>" + strings.Join(ts, ", ") + "</makefile_alvos>\n\n")
		}
	}
	if tools := checks.Detect(r.Root, files); len(tools) > 0 {
		var names []string
		for _, t := range tools {
			names = append(names, t.Name)
		}
		b.WriteString("<ferramentas_de_verificacao>" + strings.Join(names, ", ") + "</ferramentas_de_verificacao>\n\n")
	}
	if subs := r.RecentSubjects(15); len(subs) > 0 {
		b.WriteString("<commits_recentes>\n" + strings.Join(subs, "\n") + "\n</commits_recentes>\n\n")
	}
	for _, rd := range []string{"README.md", "README", "readme.md"} {
		if s := read(rd, 3000); s != "" {
			b.WriteString("<readme>\n" + s + "\n</readme>\n\n")
			break
		}
	}
	if base != "" {
		b.WriteString("<mapa>\n" + base + "\n</mapa>\n\n")
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		if s := read(f, 8000); s != "" {
			fmt.Fprintf(&b, "<arquivo_atual nome=%q>\n%s\n</arquivo_atual>\n\n", f, s)
		}
	}
	b.WriteString("Escreva o arquivo de instruções para agentes deste projeto.")
	return b.String()
}

// summarizePackageJSON fica com o nome, os scripts e os nomes das dependências.
func summarizePackageJSON(s string) string {
	var p struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
		Deps    map[string]any    `json:"dependencies"`
		DevDeps map[string]any    `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(s), &p) != nil {
		return s
	}
	keys := func(m map[string]any) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ", ")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	if len(p.Scripts) > 0 {
		var ks []string
		for k := range p.Scripts {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		b.WriteString("scripts:\n")
		for _, k := range ks {
			fmt.Fprintf(&b, "  %s: %s\n", k, p.Scripts[k])
		}
	}
	fmt.Fprintf(&b, "dependencies: %s\ndevDependencies: %s", keys(p.Deps), keys(p.DevDeps))
	return b.String()
}

func cleanRegras(text string) string {
	t := strings.TrimSpace(text)
	if m := fenceWrap.FindStringSubmatch(t); m != nil {
		t = m[1]
	}
	return strings.TrimSpace(t) + "\n"
}

// writeRegras grava o arquivo de instruções na raiz, guardando uma cópia do anterior.
func writeRegras(root, name, content string) (string, error) {
	if name == "" {
		name = "CLAUDE.md"
	}
	if name != "CLAUDE.md" && name != "AGENTS.md" {
		return "", fmt.Errorf("o arquivo deve ser CLAUDE.md ou AGENTS.md")
	}
	dst := filepath.Join(root, name)
	if old, err := os.ReadFile(dst); err == nil {
		bak := filepath.Join(dataHome(), "pedidos", repoDisplayName(root)+"-"+name+".antes")
		if os.MkdirAll(filepath.Dir(bak), 0o755) == nil {
			_ = os.WriteFile(bak, old, 0o644)
		}
	}
	return dst, os.WriteFile(dst, []byte(content), 0o644)
}

// ---- na interface ----

func (m model) startRegras(arg string) (tea.Model, tea.Cmd) {
	if f := strings.Fields(arg); len(f) > 0 && (f[0] == "salvar" || f[0] == "gravar") {
		if m.regrasDraft == "" {
			m.setInfo("gere o rascunho primeiro com /regras")
			return m, nil
		}
		name := ""
		if len(f) > 1 {
			name = f[1]
		}
		dst, err := writeRegras(m.repo.Root, name, m.regrasDraft)
		if err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		m.entries = append(m.entries, entry{kind: eNotice, text: "regras gravadas em " + prettyPath(dst) +
			" (a versão anterior, se havia, ficou nos pedidos do dante) · confira e commite"})
		m.refreshChat()
		return m, tea.Batch(loadLog(m.repo), m.reloadFilesIfWorktree())
	}
	if !m.canAsk() {
		return m, nil
	}
	compact := ""
	if m.mapData != nil {
		compact = m.mapData.Compact(4000)
	}
	return m.askIsolated(regrasSystemPrompt, "rascunho das regras para agentes (CLAUDE.md)", regrasFacts(m.repo, compact), "regras")
}

func (m *model) onRegrasDone(text string) {
	m.regrasDraft = cleanRegras(text)
	termenv.Copy(m.regrasDraft)
	p := filepath.Join(dataHome(), "pedidos", "regras.md")
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		_ = os.WriteFile(p, []byte(m.regrasDraft), 0o644)
	}
	m.setInfo("rascunho copiado e salvo em " + prettyPath(p) + " · /regras salvar grava em CLAUDE.md (/regras salvar AGENTS.md)")
}

// ---- sem interface ----

func headlessRegras(m model, save bool, name string) int {
	if err := m.needModel(); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	compact := ""
	if mp, err := mapa.Load(mapPath(m.repo.Root)); err == nil {
		compact = mp.Compact(4000)
	}
	chat, err := newIsolatedChat(m, regrasSystemPrompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	r, err := sendHeadless(m, chat, regrasFacts(m.repo, compact), !save)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	if save {
		dst, err := writeRegras(m.repo.Root, name, cleanRegras(r.text))
		if err != nil {
			fmt.Fprintln(os.Stderr, "dante:", err)
			return 2
		}
		fmt.Println("Regras gravadas em", dst, "(confira e commite)")
	}
	return 0
}
