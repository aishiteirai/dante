package repo

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Pacote de revisão: o que vai junto da primeira mensagem para o modelo revisar sem
// precisar sair buscando. O diff do código vem com 3 linhas de contexto (o cabeçalho de
// cada trecho já diz a função); testes, documentação e arquivos gerados só aparecem como
// lista, e o modelo pede o diff deles se precisar. Se o código não couber no limite, entram
// primeiro os arquivos com mais mudanças.

type FileKind int

const (
	KindCode FileKind = iota
	KindTest
	KindDoc
	KindGenerated
)

var kindWords = map[FileKind]string{KindCode: "código", KindTest: "teste", KindDoc: "documentação", KindGenerated: "gerado"}

var (
	testName = regexp.MustCompile(`(_test\.go|_test\.py|\.test\.[a-z]+|\.spec\.[a-z]+|Tests?\.(java|kt|cs|swift|php))$|^test_.*\.py$`)
	genName  = regexp.MustCompile(`(\.min\.(js|css)|\.pb\.go|_generated\.[a-z]+|\.generated\.[a-z]+|\.snap|\.lock|\.map)$`)
	docExt   = map[string]bool{".md": true, ".rst": true, ".txt": true, ".adoc": true}
	locks    = map[string]bool{"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
		"Cargo.lock": true, "poetry.lock": true, "Gemfile.lock": true, "composer.lock": true, "uv.lock": true}
)

// Kind classifica o arquivo pelo caminho.
func Kind(p string) FileKind {
	base := path.Base(p)
	dirs := "/" + path.Dir(p) + "/"
	switch {
	case locks[base] || genName.MatchString(base) || strings.Contains(dirs, "/vendor/") || strings.Contains(dirs, "/dist/"):
		return KindGenerated
	case testName.MatchString(base) || strings.Contains(dirs, "/test/") || strings.Contains(dirs, "/tests/") ||
		strings.Contains(dirs, "/__tests__/") || strings.Contains(dirs, "/testdata/") || strings.Contains(dirs, "/spec/"):
		return KindTest
	case docExt[strings.ToLower(path.Ext(base))] || strings.HasPrefix(dirs, "/docs/") ||
		strings.HasPrefix(strings.ToUpper(base), "LICENSE") || strings.HasPrefix(strings.ToUpper(base), "CHANGELOG"):
		return KindDoc
	}
	return KindCode
}

// LeftOut é um arquivo alterado que não veio no diff do pacote.
type LeftOut struct {
	Path     string
	Add, Del int
	Why      string
}

type ReviewPack struct {
	Diff     string // diffs incluídos, na ordem do git
	Included int
	Left     []LeftOut
}

// splitDiff separa o diff por arquivo (chave: o caminho novo).
func splitDiff(d string) (order []string, chunks map[string]string) {
	chunks = map[string]string{}
	parts := strings.Split("\n"+d, "\ndiff --git ")
	for _, p := range parts[1:] {
		head, _, _ := strings.Cut(p, "\n")
		name := head
		if i := strings.LastIndex(head, " b/"); i >= 0 {
			name = head[i+3:]
		}
		name = strings.Trim(name, `"`)
		order = append(order, name)
		chunks[name] = "diff --git " + p + "\n"
	}
	return
}

// ReviewDiff monta o pacote de revisão do alvo, com até `limit` bytes de diff.
func (r Repo) ReviewDiff(sha string, limit int) (ReviewPack, error) {
	files, err := r.Files(sha)
	if err != nil {
		return ReviewPack{}, err
	}
	d, err := r.DiffForModel(sha, "", 3)
	if err != nil {
		return ReviewPack{}, err
	}
	order, chunks := splitDiff(d)

	// Se o alvo não mexe em código (só testes ou docs), eles mesmos são o que revisar.
	hasCode := false
	for _, f := range files {
		if Kind(f.Path) == KindCode && chunks[f.Path] != "" {
			hasCode = true
		}
	}
	var cands []FileChange
	var pack ReviewPack
	for _, f := range files {
		k := Kind(f.Path)
		switch {
		case r.Blocked(f.Path):
			continue // sensível: o conteúdo não vai ao modelo
		case chunks[f.Path] == "" && f.Status == "?":
			pack.Left = append(pack.Left, LeftOut{f.Path, f.Add, f.Del, "novo, fora do git: leia com read_file"})
		case chunks[f.Path] == "":
			pack.Left = append(pack.Left, LeftOut{f.Path, f.Add, f.Del, "sem diff de texto"})
		case k == KindGenerated || (hasCode && k != KindCode):
			pack.Left = append(pack.Left, LeftOut{f.Path, f.Add, f.Del, kindWords[k]})
		default:
			cands = append(cands, f)
		}
	}
	// Os que mais mudaram primeiro, até o limite.
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Add+cands[i].Del > cands[j].Add+cands[j].Del })
	in := map[string]bool{}
	size := 0
	for _, f := range cands {
		c := chunks[f.Path]
		if size+len(c) > limit {
			pack.Left = append(pack.Left, LeftOut{f.Path, f.Add, f.Del, "não coube aqui"})
			continue
		}
		in[f.Path] = true
		size += len(c)
	}
	var b strings.Builder
	for _, p := range order {
		if in[p] {
			b.WriteString(chunks[p])
			pack.Included++
		}
	}
	pack.Diff = b.String()
	sort.SliceStable(pack.Left, func(i, j int) bool { return pack.Left[i].Path < pack.Left[j].Path })
	return pack, nil
}

// ---- esqueleto de arquivos grandes ----

// OutlineLines: arquivos maiores que isto, lidos sem trecho, voltam como esqueleto.
const OutlineLines = 250

const maxOutline = 150

var declRes = map[string]*regexp.Regexp{
	".go":   regexp.MustCompile(`^(func|type|var|const)\b`),
	".py":   regexp.MustCompile(`^\s*(async\s+)?(def|class)\s`),
	".rb":   regexp.MustCompile(`^\s*(def|class|module)\s`),
	".rs":   regexp.MustCompile(`^\s*(pub(\([a-z]+\))?\s+)?(async\s+)?(fn|struct|enum|trait|impl|mod|type)\b`),
	".java": regexp.MustCompile(`^\s*(public|private|protected|static|final|abstract|class|interface|enum|record)\b.*[({]\s*$`),
	".kt":   regexp.MustCompile(`^\s*((public|private|internal|override|open|data|sealed|suspend)\s+)*(fun|class|object|interface)\b`),
	".cs":   regexp.MustCompile(`^\s*(public|private|protected|internal|static|class|interface|enum|record)\b.*[({]?\s*$`),
	".php":  regexp.MustCompile(`^\s*((public|private|protected|static|abstract|final)\s+)*(function|class|interface|trait)\b`),
	".md":   regexp.MustCompile(`^#{1,4} `),
	".js":   regexp.MustCompile(`^\s{0,2}(export\s+)?(default\s+)?(async\s+)?(function|class|interface|type|enum)\b|^(export\s+)?(const|let)\s+\w+\s*=\s*(async\s*)?(\(|function)`),
}

func init() {
	for _, e := range []string{".ts", ".tsx", ".jsx", ".mjs", ".cjs"} {
		declRes[e] = declRes[".js"]
	}
}

// outline devolve as linhas de declaração do arquivo, numeradas.
func outline(p string, lines []string) string {
	re := declRes[strings.ToLower(path.Ext(p))]
	var out []string
	more := 0
	for i, l := range lines {
		t := strings.TrimRight(l, " \t\r")
		var ok bool
		if re != nil {
			ok = re.MatchString(t)
		} else { // sem regra para a linguagem: linhas sem recuo que não fecham blocos
			ok = t != "" && t[0] != ' ' && t[0] != '\t' && !strings.ContainsAny(t[:1], "})]#*/") && !strings.HasPrefix(t, "//")
		}
		if !ok {
			continue
		}
		if len(out) >= maxOutline {
			more++
			continue
		}
		if len(t) > 160 {
			t = t[:160] + "…"
		}
		out = append(out, fmt.Sprintf("%5d  %s", i+1, t))
	}
	s := strings.Join(out, "\n")
	if more > 0 {
		s += fmt.Sprintf("\n… e mais %d declarações", more)
	}
	return s
}
