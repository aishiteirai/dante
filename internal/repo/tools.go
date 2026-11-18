package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
)

const (
	maxFileLines  = 1500
	maxOutputSize = 60_000
	maxBatchFiles = 20

	// DiffContextLines é o contexto padrão dos diffs enviados ao modelo. Com 3 linhas o
	// diff fica ~30% menor que com 10, e o cabeçalho de cada trecho já diz a função; o
	// modelo pede mais contexto (context_lines) ou o trecho do arquivo quando precisar.
	DiffContextLines = 3
	maxDiffContext   = 40
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
	file := map[string]any{
		"path":       str("Caminho a partir da raiz."),
		"ref":        str("Commit a ler (vazio = disco)."),
		"start_line": integer("Primeira linha."),
		"end_line":   integer("Última linha."),
	}
	return []llm.Tool{
		{Name: "read_file", Description: fmt.Sprintf("Lê um arquivo, com números de linha. Arquivo com mais de %d linhas, "+
			"sem start_line/end_line, volta como esqueleto (declarações e linhas): depois peça o trecho.", OutlineLines),
			Params: obj(map[string]any{"path": file["path"], "ref": file["ref"], "start_line": file["start_line"],
				"end_line": file["end_line"], "inteiro": map[string]any{"type": "boolean", "description": "Lê tudo mesmo se grande (caro)."}}, "path")},
		{Name: "read_files", Description: fmt.Sprintf("Lê até %d arquivos ou trechos numa chamada. Prefira a várias read_file: "+
			"cada rodada reenvia a conversa inteira.", maxBatchFiles),
			Params: obj(map[string]any{"files": map[string]any{"type": "array", "items": obj(file, "path")}}, "files")},
		{Name: "git_diff", Description: fmt.Sprintf("Diff do alvo (ou de `ref`), opcionalmente de um arquivo. Contexto padrão: %d linhas.", DiffContextLines),
			Params: obj(map[string]any{
				"ref":           str("Commit (vazio = alvo da conversa)."),
				"path":          str("Arquivo."),
				"context_lines": integer(fmt.Sprintf("Linhas de contexto (até %d).", maxDiffContext)),
			})},
		{Name: "grep", Description: "Busca uma regex (git grep -E) nos arquivos versionados; devolve arquivo:linha:texto.",
			Params: obj(map[string]any{"pattern": str("Regex."), "path": str("Pasta ou arquivo.")}, "pattern")},
		{Name: "list_files", Description: "Lista os arquivos versionados de uma pasta.",
			Params: obj(map[string]any{"path": str("Pasta.")})},
		{Name: "git_log", Description: "Commits, um por linha, opcionalmente de um caminho.",
			Params: obj(map[string]any{"path": str("Arquivo ou pasta."), "limit": integer("Quantidade (padrão 20).")})},
		{Name: "git_blame", Description: "Autor e commit de cada linha de um trecho.",
			Params: obj(map[string]any{"path": file["path"], "start_line": file["start_line"], "end_line": file["end_line"]},
				"path", "start_line", "end_line")},
	}
}

type fileArg struct {
	Path      string `json:"path"`
	Ref       string `json:"ref"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Whole     bool   `json:"inteiro"`
}

type toolArgs struct {
	Files     []fileArg `json:"files"`
	Path      string    `json:"path"`
	Ref       string    `json:"ref"`
	Pattern   string    `json:"pattern"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
	Whole     bool      `json:"inteiro"`
	Limit     int       `json:"limit"`
	Context   int       `json:"context_lines"`
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
			if isGitDir(clean) { // pode guardar tokens (URLs de remotes, credenciais)
				return "a pasta .git não é lida; use git_log, git_diff e git_blame", true
			}
		}
		var out string
		var err error
		switch call.Name {
		case "read_file":
			out, err = r.readFile(a)
		case "read_files":
			out, err = r.readFiles(a.Files)
		case "git_diff":
			ref := a.Ref
			if ref == "" {
				ref = target()
			}
			n := a.Context
			if n <= 0 {
				n = DiffContextLines
			}
			out, err = r.DiffContext(ref, a.Path, min(n, maxDiffContext))
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
			out = capLines(r.filterGrep(out), maxGrepLines,
				"refine o padrão ou limite com path para ver os outros")
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

// readFiles lê vários arquivos; o erro de um arquivo não impede os outros.
func (r Repo) readFiles(files []fileArg) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("`files` precisa de pelo menos um arquivo")
	}
	if len(files) > maxBatchFiles {
		return "", fmt.Errorf("no máximo %d arquivos por chamada (vieram %d)", maxBatchFiles, len(files))
	}
	var b strings.Builder
	for _, f := range files {
		header := f.Path
		if f.StartLine > 0 || f.EndLine > 0 {
			header += fmt.Sprintf(" (linhas %d-%d)", max(f.StartLine, 1), f.EndLine)
		}
		if f.Ref != "" {
			header += " @ " + f.Ref
		}
		fmt.Fprintf(&b, "=== %s ===\n", header)
		content, err := r.readOne(f)
		if err != nil {
			fmt.Fprintf(&b, "ERRO: %s\n\n", err)
			continue
		}
		b.WriteString(strings.TrimRight(content, "\n") + "\n\n")
	}
	return b.String(), nil
}

// readOne aplica a um arquivo do lote as mesmas regras de read_file.
func (r Repo) readOne(f fileArg) (string, error) {
	if err := checkArg(f.Ref); err != nil {
		return "", err
	}
	clean, err := r.safePath(f.Path)
	if err != nil {
		return "", err
	}
	if r.Blocked(clean) {
		return "", blockedErr(clean)
	}
	if isGitDir(clean) {
		return "", fmt.Errorf("a pasta .git não é lida")
	}
	return r.readFile(toolArgs{Path: clean, Ref: f.Ref, StartLine: f.StartLine, EndLine: f.EndLine, Whole: f.Whole})
}

func checkArg(s string) error {
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("valor inválido: %q", s)
	}
	return nil
}

// safePath garante que o caminho fica dentro do repositório. Trata "/" e "\" como
// separadores em qualquer sistema (no Windows, filepath.Clean usa "\") e devolve o
// caminho no formato do git, com "/", que também funciona nas funções de arquivo do Windows.
func (r Repo) safePath(p string) (string, error) {
	if err := checkArg(p); err != nil {
		return "", err
	}
	outside := fmt.Errorf("caminho fora do repositório: %s", p)
	native := filepath.FromSlash(p)
	if filepath.IsAbs(native) || filepath.VolumeName(native) != "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		rel, err := filepath.Rel(r.Root, filepath.Clean(native))
		if err != nil {
			return "", outside
		}
		p = rel
	}
	clean := path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", outside
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
	// Arquivo grande sem trecho: o esqueleto, para o modelo pedir só o que interessa.
	if a.StartLine <= 0 && a.EndLine <= 0 && !a.Whole && len(lines) > OutlineLines {
		if o := outline(a.Path, lines); o != "" {
			return fmt.Sprintf("(%s tem %d linhas: este é o esqueleto, com as declarações e suas linhas. Peça o trecho "+
				"que interessa com start_line/end_line, de preferência vários de uma vez em read_files.)\n%s\n",
				a.Path, len(lines), o), nil
		}
	}
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

// maxGrepLines: resultados do grep devolvidos ao modelo; o resto vira uma contagem.
const maxGrepLines = 40

// capLines corta a saída em n linhas, dizendo quantas ficaram de fora.
func capLines(s string, n int, hint string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… e mais %d resultados (%s)", len(lines)-n, hint)
}

func truncate(s string) string {
	if len(s) <= maxOutputSize {
		return s
	}
	return s[:maxOutputSize] + fmt.Sprintf("\n… (saída cortada: %d de %d caracteres)", maxOutputSize, len(s))
}
