package tech

import (
	"fmt"
	"strings"
)

var kindNames = map[string]string{
	"linguagem": "Linguagens", "framework": "Frameworks", "interface": "Interface", "http": "Web e rede",
	"banco": "Banco de dados", "ia": "IA", "teste": "Testes", "ferramenta": "Ferramentas", "biblioteca": "Bibliotecas",
}

var kindOrder = []string{"linguagem", "framework", "interface", "http", "banco", "ia", "teste", "ferramenta", "biblioteca"}

// Markdown é o relatório para a pessoa: linguagens, as tecnologias por tipo com o que são e
// onde aparecem, e as outras dependências só pelo nome.
func (r Report) Markdown() string {
	var b strings.Builder
	if len(r.Languages) > 0 {
		var ls []string
		for i, l := range r.Languages {
			if i == 6 {
				break
			}
			ls = append(ls, fmt.Sprintf("%s (%d)", l.Name, l.Files))
		}
		b.WriteString("**Arquivos por linguagem:** " + strings.Join(ls, " · ") + "\n\n")
	}
	var others []string
	for _, k := range kindOrder {
		var lines []string
		for _, t := range r.Techs {
			if t.Kind != k {
				continue
			}
			if t.What == "" {
				others = append(others, t.Name)
				continue
			}
			line := "- **" + t.Name + "**"
			if t.Version != "" && t.Kind == "linguagem" {
				line += " " + t.Version
			}
			line += ": " + t.What
			if t.Dev {
				line += " _(desenvolvimento)_"
			}
			if t.Uses > 0 {
				line += fmt.Sprintf(". Em %d arquivo(s), como `%s`", t.Uses, t.Files[0])
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			b.WriteString("**" + kindNames[k] + "**\n" + strings.Join(lines, "\n") + "\n\n")
		}
	}
	if len(others) > 0 {
		b.WriteString("**Outras dependências:** " + strings.Join(others, ", ") + "\n\n")
	}
	if len(r.Techs) == 0 && len(r.Languages) == 0 {
		b.WriteString("Não achei manifestos (go.mod, package.json, pyproject.toml…) nem código conhecido.\n\n")
	}
	b.WriteString("_Lido dos manifestos e do código, sem IA. `/trilha NOME` monta um roteiro para aprender uma delas por este projeto._")
	return b.String()
}

// Compact é a lista curta para ir num prompt: nome e pacote, uma por linha.
func (r Report) Compact(n int) string {
	var b strings.Builder
	for i, t := range r.Techs {
		if i == n {
			break
		}
		fmt.Fprintf(&b, "- %s", t.Name)
		if t.Package != "" && t.Package != strings.ToLower(t.Name) {
			fmt.Fprintf(&b, " (%s)", t.Package)
		}
		b.WriteString("\n")
	}
	return b.String()
}
