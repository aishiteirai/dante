package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/charmbracelet/lipgloss"
)

// Achados de revisão em cartões: o modelo registra cada problema pela ferramenta
// registrar_achado, e o chat desenha um cartão por achado.

const findingTool = "registrar_achado"

type finding struct {
	Arquivo    string `json:"arquivo"`
	Linha      int    `json:"linha"`
	Severidade string `json:"severidade"`
	Categoria  string `json:"categoria"`
	Titulo     string `json:"titulo"`
	Explicacao string `json:"explicacao"`
	Sugestao   string `json:"sugestao"`
}

var severities = []string{"critica", "alta", "media", "baixa"}
var categories = []string{"bug", "seguranca", "qualidade", "padroes"}

func findingToolDef() llm.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	enum := func(d string, vals []string) map[string]any {
		e := map[string]any{"type": "string", "enum": vals}
		if d != "" {
			e["description"] = d
		}
		return e
	}
	return llm.Tool{
		Name: findingTool,
		Description: "Registra um problema da revisão (vira um cartão para o desenvolvedor). Um por problema, do mais grave " +
			"ao menos grave, todos na mesma resposta.",
		Params: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"arquivo":    str("Caminho a partir da raiz."),
				"linha":      map[string]any{"type": "integer", "description": "Linha no arquivo novo."},
				"severidade": enum("critica: perda de dados ou brecha; alta: bug no uso normal; media: caso de borda; baixa: melhoria.", severities),
				"categoria":  enum("", categories),
				"titulo":     str("Até ~70 caracteres."),
				"explicacao": str("O problema e a situação concreta que leva a ele."),
				"sugestao":   str("Correção proposta (opcional)."),
			},
			"required": []string{"arquivo", "linha", "severidade", "titulo", "explicacao"},
		},
	}
}

// parseFinding valida os argumentos; o erro volta ao modelo para ele corrigir.
func parseFinding(args string) (finding, error) {
	var f finding
	if err := json.Unmarshal([]byte(args), &f); err != nil {
		return f, fmt.Errorf("argumentos inválidos (JSON): %v", err)
	}
	f.Severidade = fold(strings.TrimSpace(f.Severidade))
	f.Categoria = fold(strings.TrimSpace(f.Categoria))
	switch {
	case strings.TrimSpace(f.Arquivo) == "":
		return f, fmt.Errorf("`arquivo` é obrigatório")
	case strings.TrimSpace(f.Titulo) == "":
		return f, fmt.Errorf("`titulo` é obrigatório")
	case !oneOf(severities, f.Severidade):
		return f, fmt.Errorf("`severidade` deve ser uma de: %s", strings.Join(severities, ", "))
	}
	if f.Categoria != "" && !oneOf(categories, f.Categoria) {
		f.Categoria = "qualidade"
	}
	if f.Categoria == "" {
		f.Categoria = "bug"
	}
	f.Linha = max(f.Linha, 0)
	return f, nil
}

func oneOf[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// withFindings envolve o executor do repositório: registrar_achado é tratado aqui.
func withFindings(exec llm.Executor) llm.Executor {
	return func(ctx context.Context, call llm.ToolCall) (string, bool) {
		if call.Name != findingTool {
			return exec(ctx, call)
		}
		if _, err := parseFinding(call.Args); err != nil {
			return err.Error(), true
		}
		return "Achado registrado e mostrado ao desenvolvedor.", false
	}
}

// ---- desenho ----

var sevLabel = map[string]string{"critica": "CRÍTICA", "alta": "ALTA", "media": "MÉDIA", "baixa": "BAIXA"}
var catLabel = map[string]string{"bug": "bug", "seguranca": "segurança", "qualidade": "qualidade", "padroes": "padrões do time"}

func sevColor(s string) lipgloss.TerminalColor {
	switch s {
	case "critica":
		return errColor
	case "alta":
		return cc("#bc4c00", "166", "9", "#ff9e64", "209", "9")
	case "media":
		return warnCol
	}
	return cc("#0969da", "25", "4", "#7aa2f7", "111", "12")
}

func refChip(f finding, n int) string {
	loc := f.Arquivo
	if f.Linha > 0 {
		loc = fmt.Sprintf("%s:%d", shortRefPath(f.Arquivo), f.Linha)
	}
	if n > 0 {
		loc = fmt.Sprintf("[%d] %s", n, loc)
	}
	return lipgloss.NewStyle().Foreground(refColor).Render(loc)
}

// shortRefPath encurta caminhos longos para o cartão: …/llm/openai.go.
func shortRefPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return "…/" + strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}

// renderFinding desenha o cartão: barra colorida à esquerda, gravidade e local, título,
// explicação e sugestão.
func renderFinding(f finding, n, w int) string {
	col := sevColor(f.Severidade)
	bar := lipgloss.NewStyle().Foreground(col).Render("▌")
	inner := max(w-2, 10)
	head := lipgloss.NewStyle().Foreground(col).Bold(true).Render(sevLabel[f.Severidade] + " · " + catLabel[f.Categoria])
	chip := refChip(f, n)
	gap := inner - lipgloss.Width(head) - lipgloss.Width(chip)
	if gap < 1 {
		head = head + "\n" + chip
	} else {
		head = head + strings.Repeat(" ", gap) + chip
	}
	body := []string{head, boldStyle.Width(inner).Render(f.Titulo)}
	if f.Explicacao != "" {
		body = append(body, lipgloss.NewStyle().Width(inner).Render(f.Explicacao))
	}
	if s := strings.TrimSpace(f.Sugestao); s != "" {
		if strings.Contains(s, "```") || strings.Contains(s, "\n") {
			body = append(body, subtleStyle.Render("Sugestão:"), markdown(s, inner))
		} else {
			body = append(body, lipgloss.NewStyle().Width(inner).Render(subtleStyle.Render("Sugestão: ")+s))
		}
	}
	var out []string
	for _, l := range strings.Split(strings.Join(body, "\n"), "\n") {
		out = append(out, bar+" "+l)
	}
	return strings.Join(out, "\n")
}

func findingMarkdown(f finding, n int) string {
	s := fmt.Sprintf("### [%d] %s · %s — %s\n\n`%s:%d`\n\n%s\n", n, sevLabel[f.Severidade], catLabel[f.Categoria], f.Titulo, f.Arquivo, f.Linha, f.Explicacao)
	if f.Sugestao != "" {
		s += "\n**Sugestão:** " + f.Sugestao + "\n"
	}
	return s
}
