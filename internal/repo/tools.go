package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
)

const (
	maxFileLines  = 1500
	maxOutputSize = 60_000
)

// Tools devolve as ferramentas de leitura oferecidas ao modelo.
func Tools() []llm.Tool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	integer := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	obj := func(props map[string]any, req ...string) map[string]any {
		if req == nil {
			req = []string{}
		}
		return map[string]any{"type": "object", "properties": props, "required": req}
	}
	return []llm.Tool{
		{Name: "read_file", Description: "Abre um arquivo do repositório e devolve o conteúdo com números de linha. " +
			"Sem `ref`, lê a versão atual no disco; com `ref` (SHA de commit), lê a versão daquele commit.",
			Params: obj(map[string]any{
				"path":       str("Caminho relativo à raiz do repositório."),
				"ref":        str("Opcional: commit cuja versão deve ser lida."),
				"start_line": integer("Opcional: primeira linha (1 = início)."),
				"end_line":   integer("Opcional: última linha."),
			}, "path")},
		{Name: "git_diff", Description: "Diff do alvo em revisão (commit ou mudanças locais). Use `path` para um arquivo só.",
			Params: obj(map[string]any{
				"ref":  str("Opcional: SHA do commit; vazio = alvo atual da conversa."),
				"path": str("Opcional: arquivo."),
			})},
		{Name: "grep", Description: "Busca um padrão (regex estendida do git grep) nos arquivos versionados. Devolve arquivo:linha:texto.",
			Params: obj(map[string]any{
				"pattern": str("Expressão a buscar."),
				"path":    str("Opcional: limita a um diretório ou arquivo."),
			}, "pattern")},
		{Name: "list_files", Description: "Lista arquivos versionados, opcionalmente dentro de um diretório.",
			Params: obj(map[string]any{"path": str("Opcional: diretório.")})},
		{Name: "git_log", Description: "Histórico de commits (um por linha), opcionalmente de um arquivo.",
			Params: obj(map[string]any{
				"path":  str("Opcional: arquivo ou diretório."),
				"limit": integer("Opcional: quantidade (padrão 20)."),
			})},
		{Name: "git_blame", Description: "Quem alterou cada linha de um trecho do arquivo, e em qual commit.",
			Params: obj(map[string]any{
				"path":       str("Arquivo."),
				"start_line": integer("Primeira linha."),
				"end_line":   integer("Última linha."),
			}, "path", "start_line", "end_line")},
	}
}

type toolArgs struct {
	Path      string `json:"path"`
	Ref       string `json:"ref"`
	Pattern   string `json:"pattern"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Limit     int    `json:"limit"`
}

// Executor devolve o executor das ferramentas; `target` é o SHA em revisão (WorkTree = mudanças locais).
func (r Repo) Executor(target func() string) llm.Executor {
	return func(ctx context.Context, call llm.ToolCall) (string, bool) {
		var a toolArgs
		if err := json.Unmarshal([]byte(call.Args), &a); err != nil {
			return "argumentos inválidos (JSON): " + err.Error(), true
		}
		if err := checkArg(a.Ref); err != nil {
			return err.Error(), true
		}
		if a.Path != "" {
			clean, err := r.safePath(a.Path)
			if err != nil {
				return err.Error(), true
			}
			a.Path = clean
			if r.Blocked(clean) && call.Name != "list_files" && call.Name != "git_log" {
				return blockedErr(clean).Error(), true
			}
		}
		var out string
		var err error
		switch call.Name {
		case "read_file":
			out, err = r.readFile(a)
		case "git_diff":
			ref := a.Ref
			if ref == "" {
				ref = target()
			}
			out, err = r.Diff(ref, a.Path)
			out = r.filterDiff(out)
			if err == nil && strings.TrimSpace(out) == "" {
				out = "(sem diferenças)"
			}
		case "grep":
			if a.Pattern == "" {
				return "`pattern` é obrigatório", true
			}
			args := []string{"grep", "-n", "-I", "-E", "--untracked", "-e", a.Pattern}
			if a.Path != "" {
				args = append(args, "--", a.Path)
			}
			out, err = r.git(args...)
			if err != nil && out == "" && !strings.Contains(err.Error(), "fatal") {
				out, err = "(nenhum resultado)", nil
			}
			out = r.filterGrep(out)
		case "list_files":
			args := []string{"ls-files"}
			if a.Path != "" {
				args = append(args, "--", a.Path)
			}
			out, err = r.git(args...)
			out = r.markBlocked(out)
		case "git_log":
			n := a.Limit
			if n <= 0 || n > 200 {
				n = 20
			}
			args := []string{"log", fmt.Sprintf("-n%d", n), "--format=%h %ad %an: %s", "--date=short"}
			if a.Path != "" {
				args = append(args, "--", a.Path)
			}
			out, err = r.git(args...)
		case "git_blame":
			if a.Path == "" || a.StartLine < 1 || a.EndLine < a.StartLine {
				return "informe path, start_line e end_line válidos", true
			}
			out, err = r.git("blame", "-L", fmt.Sprintf("%d,%d", a.StartLine, a.EndLine), "--date=short", "--", a.Path)
		default:
			return "ferramenta desconhecida: " + call.Name, true
		}
		if err != nil {
			return err.Error(), true
		}
		out, n := Redact(out)
		if n > 0 {
			out += fmt.Sprintf("\n(%d valor(es) com cara de segredo foram mascarados pelo dante)", n)
		}
		return truncate(out), false
	}
}

func (r Repo) markBlocked(list string) string {
	lines := strings.Split(list, "\n")
	for i, l := range lines {
		if l != "" && r.Blocked(l) {
			lines[i] = l + "  (sensível: bloqueado para leitura)"
		}
	}
	return strings.Join(lines, "\n")
}

func checkArg(s string) error {
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("valor inválido: %q", s)
	}
	return nil
}

// safePath garante que o caminho fica dentro do repositório.
func (r Repo) safePath(p string) (string, error) {
	if err := checkArg(p); err != nil {
		return "", err
	}
	clean := filepath.Clean(strings.TrimPrefix(p, "./"))
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(r.Root, clean)
		if err != nil {
			return "", fmt.Errorf("caminho fora do repositório: %s", p)
		}
		clean = rel
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("caminho fora do repositório: %s", p)
	}
	return clean, nil
}

func (r Repo) readFile(a toolArgs) (string, error) {
	if a.Path == "" || a.Path == "." {
		return "", fmt.Errorf("`path` é obrigatório")
	}
	var data string
	if a.Ref != "" {
		out, err := r.git("show", a.Ref+":"+a.Path)
		if err != nil {
			return "", err
		}
		data = out
	} else {
		full := filepath.Join(r.Root, a.Path)
		// Resolve links simbólicos para não sair do repositório.
		if real, err := filepath.EvalSymlinks(full); err == nil {
			rootReal, _ := filepath.EvalSymlinks(r.Root)
			if real != rootReal && !strings.HasPrefix(real, rootReal+string(os.PathSeparator)) {
				return "", fmt.Errorf("caminho fora do repositório: %s", a.Path)
			}
		}
		st, err := os.Stat(full)
		if err != nil {
			return "", fmt.Errorf("arquivo não encontrado: %s", a.Path)
		}
		if st.IsDir() {
			return "", fmt.Errorf("%s é um diretório; use list_files", a.Path)
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		data = string(b)
	}
	if strings.ContainsRune(data[:min(len(data), 8000)], 0) {
		return "(arquivo binário)", nil
	}
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	start, end := max(a.StartLine, 1), a.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return fmt.Sprintf("(o arquivo tem %d linhas)", len(lines)), nil
	}
	note := ""
	if end-start+1 > maxFileLines {
		end = start + maxFileLines - 1
		note = fmt.Sprintf("\n… (mostrando até a linha %d de %d; peça outro trecho com start_line/end_line)", end, len(lines))
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%5d  %s\n", i, lines[i-1])
	}
	return b.String() + note, nil
}

func truncate(s string) string {
	if len(s) <= maxOutputSize {
		return s
	}
	return s[:maxOutputSize] + fmt.Sprintf("\n… (saída cortada: %d de %d caracteres)", maxOutputSize, len(s))
}
