package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	"github.com/muesli/termenv"
)

// "Pedir a correção ao Claude": a partir dos achados da revisão, um pedido pronto para colar
// no Claude Code (ou em outro agente), com o arquivo, a linha, o problema e a sugestão. Vai
// para a área de transferência (OSC 52, funciona até por SSH) e para um arquivo.

// findingByRef devolve o achado da referência n (nil se a referência não é de um achado).
func (m model) findingByRef(n int) *finding {
	for _, e := range m.entries {
		if e.kind == eFinding && e.ref == n {
			return e.f
		}
	}
	return nil
}

func (m model) allFindings() []finding {
	var out []finding
	for _, e := range m.entries {
		if e.kind == eFinding {
			out = append(out, *e.f)
		}
	}
	return out
}

// fixRequest monta o pedido de correção.
func (m model) fixRequest(fs []finding) string {
	var b strings.Builder
	where := "nas mudanças locais"
	switch base, tip, ok := repo.SplitRange(m.target); {
	case ok:
		where = fmt.Sprintf("na branch %s (comparada com %s)", tip, base)
	case m.target != repo.WorkTree:
		where = "no commit " + shortSHA(m.target)
	}
	if len(fs) == 1 {
		fmt.Fprintf(&b, "Corrija este problema encontrado na revisão %s:\n\n", where)
	} else {
		fmt.Fprintf(&b, "Corrija estes %d problemas encontrados na revisão %s, na ordem:\n\n", len(fs), where)
	}
	for i, f := range fs {
		if len(fs) > 1 {
			fmt.Fprintf(&b, "%d. ", i+1)
		}
		fmt.Fprintf(&b, "**%s** (%s, %s) em `%s:%d`\n", f.Titulo, sevLabel[f.Severidade], catLabel[f.Categoria], f.Arquivo, f.Linha)
		if f.Explicacao != "" {
			b.WriteString("   " + strings.ReplaceAll(f.Explicacao, "\n", "\n   ") + "\n")
		}
		if s := strings.TrimSpace(f.Sugestao); s != "" {
			b.WriteString("   Sugestão: " + strings.ReplaceAll(s, "\n", "\n   ") + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Confira cada ponto no código antes de mudar (se algum não proceder, diga por quê). Siga o estilo do " +
		"projeto, mude só o necessário e rode os testes no fim.\n")
	return b.String()
}

// copyFixRequest copia o pedido e o salva em ~/.local/share/dante/pedidos/ultimo.md.
func (m *model) copyFixRequest(fs []finding) {
	if len(fs) == 0 {
		m.setInfo("não há achado para pedir a correção")
		return
	}
	text := m.fixRequest(fs)
	termenv.Copy(text)
	p := filepath.Join(dataHome(), "pedidos", "ultimo.md")
	saved := ""
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil && os.WriteFile(p, []byte(text), 0o644) == nil {
		saved = " · também em " + prettyPath(p)
	}
	m.setInfo(fmt.Sprintf("pedido de correção (%s) copiado: cole no Claude%s",
		plural(len(fs), "achado", "achados"), saved))
}
