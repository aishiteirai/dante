// Package mapa gera o mapa de um repositório: um resumo por pasta, feito por um modelo
// barato, e uma visão geral com por onde começar, o fluxo principal e um glossário. O mapa
// fica salvo por repositório e é atualizado só nas pastas que mudaram. Não depende da
// interface, para poder rodar também como comando (dante mapa) ou num painel do noct.
package mapa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
)

const (
	maxFileBytes  = 150_000   // arquivos maiores ficam fora (gerados, dados)
	maxScanBytes  = 8_000_000 // teto da varredura inteira
	maxChunkBytes = 60_000    // conteúdo por chamada ao modelo (~17 mil tokens)
	bytesPerToken = 3.5
	callOverhead  = 600  // instruções de cada chamada, em tokens
	folderOutput  = 700  // resposta de uma pasta, em tokens
	overviewOut   = 1500 // resposta da visão geral
	formatVersion = 1
)

// Arquivos que não ajudam a entender o projeto.
var skipNames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "Cargo.lock": true,
	"poetry.lock": true, "Gemfile.lock": true, "composer.lock": true, "uv.lock": true, "bun.lockb": true,
}

type File struct {
	Path    string
	content []byte
}

// Folder é um grupo de arquivos resumido numa chamada (ou em partes, se for grande).
type Folder struct {
	Dir   string
	Files []File
	Bytes int
	Hash  string
}

// Scan é a varredura do repositório.
type Scan struct {
	Folders []Folder
	Files   int
	Bytes   int
	Skipped int // arquivos deixados de fora (sensíveis, binários, grandes, travas)
	readme  string
}

// folderKey agrupa por até dois níveis de pasta: "cmd/dante", "internal/llm", ".".
func folderKey(p string) string {
	d := path.Dir(p)
	if d == "." {
		return "."
	}
	parts := strings.Split(d, "/")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, "/")
}

// ScanRepo lê os arquivos do repositório (como estão no disco), sem os sensíveis.
func ScanRepo(r repo.Repo) (Scan, error) {
	paths, err := r.ListFiles()
	if err != nil {
		return Scan{}, err
	}
	sort.Strings(paths)
	var s Scan
	groups := map[string]*Folder{}
	var order []string
	for _, p := range paths {
		if r.Blocked(p) || skipNames[path.Base(p)] || strings.HasPrefix(p, ".git/") {
			s.Skipped++
			continue
		}
		full := filepath.Join(r.Root, filepath.FromSlash(p))
		st, err := os.Lstat(full)
		if err != nil || !st.Mode().IsRegular() || st.Size() > maxFileBytes || s.Bytes+int(st.Size()) > maxScanBytes {
			s.Skipped++
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 { // binário
			s.Skipped++
			continue
		}
		red, _ := repo.Redact(string(b))
		b = []byte(red)
		if s.readme == "" && strings.EqualFold(strings.TrimSuffix(p, path.Ext(p)), "readme") {
			s.readme = string(b[:min(len(b), 6000)])
		}
		k := folderKey(p)
		g, ok := groups[k]
		if !ok {
			g = &Folder{Dir: k}
			groups[k] = g
			order = append(order, k)
		}
		g.Files = append(g.Files, File{Path: p, content: b})
		g.Bytes += len(b)
		s.Files++
		s.Bytes += len(b)
	}
	sort.Strings(order)
	for _, k := range order {
		g := groups[k]
		h := sha256.New()
		for _, f := range g.Files {
			fmt.Fprintf(h, "%s\x00%d\x00", f.Path, len(f.content))
			h.Write(f.content)
		}
		g.Hash = hex.EncodeToString(h.Sum(nil))[:16]
		s.Folders = append(s.Folders, *g)
	}
	return s, nil
}

// ---- o mapa salvo ----

type FolderSummary struct {
	Dir     string `json:"pasta"`
	Hash    string `json:"hash"`
	Files   int    `json:"arquivos"`
	Summary string `json:"resumo"`
}

type Map struct {
	Version   int             `json:"versao"`
	Commit    string          `json:"commit"`
	Model     string          `json:"modelo"`
	Generated time.Time       `json:"gerado_em"`
	Folders   []FolderSummary `json:"pastas"`
	Overview  string          `json:"visao_geral"`
}

func Load(p string) (*Map, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m Map
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != formatVersion {
		return nil, fmt.Errorf("mapa de uma versão antiga do dante")
	}
	return &m, nil
}

func (m *Map) Save(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (m *Map) folder(dir string) *FolderSummary {
	if m == nil {
		return nil
	}
	for i := range m.Folders {
		if m.Folders[i].Dir == dir {
			return &m.Folders[i]
		}
	}
	return nil
}

// ---- o plano: o que precisa ser resumido ----

type Step struct {
	Label  string
	Folder *Folder // nil = visão geral
	Calls  int
}

type Plan struct {
	Steps   []Step
	Reused  int // pastas que não mudaram
	Removed int // pastas que sumiram
	Tokens  int // estimativa de tokens (entrada + saída)
	scan    Scan
	old     *Map
	limit   int
}

// chunkLimit é o conteúdo por chamada, menor em modelos de contexto curto.
func chunkLimit(ctxTokens int) int {
	if ctxTokens > 0 {
		return max(min(maxChunkBytes, ctxTokens*2), 4000)
	}
	return maxChunkBytes
}

// MakePlan compara a varredura com o mapa salvo (nil = mapa novo).
func MakePlan(s Scan, old *Map, ctxTokens int) Plan {
	p := Plan{scan: s, old: old, limit: chunkLimit(ctxTokens)}
	in := map[string]bool{}
	for i := range s.Folders {
		f := &s.Folders[i]
		in[f.Dir] = true
		if o := old.folder(f.Dir); o != nil && o.Hash == f.Hash {
			p.Reused++
			continue
		}
		parts := len(chunks(*f, p.limit))
		p.Steps = append(p.Steps, Step{Label: f.Dir, Folder: f, Calls: parts})
		p.Tokens += int(float64(f.Bytes)/bytesPerToken*1.12) + parts*(callOverhead+folderOutput)
	}
	if old != nil {
		for _, f := range old.Folders {
			if !in[f.Dir] {
				p.Removed++
			}
		}
	}
	if len(p.Steps) > 0 || p.Removed > 0 || old == nil {
		p.Steps = append(p.Steps, Step{Label: "visão geral, fluxo e glossário", Calls: 1})
		p.Tokens += callOverhead + overviewOut + len(s.Folders)*folderOutput + len(s.readme)/4
	}
	return p
}

func (p Plan) UpToDate() bool { return len(p.Steps) == 0 }
func (p Plan) Files() int     { return p.scan.Files }
func (p Plan) Bytes() int     { return p.scan.Bytes }
func (p Plan) Folders() int   { return len(p.scan.Folders) }
func (p Plan) Skipped() int   { return p.scan.Skipped }

// chunks divide a pasta em partes que cabem numa chamada.
func chunks(f Folder, limit int) [][]File {
	var out [][]File
	var cur []File
	size := 0
	for _, fl := range f.Files {
		if len(fl.content) > limit {
			fl.content = append(fl.content[:limit:limit], []byte("\n… (arquivo cortado aqui)\n")...)
		}
		if size+len(fl.content) > limit && len(cur) > 0 {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, fl)
		size += len(fl.content)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// ---- geração ----

type EventState int

const (
	StepStarted EventState = iota
	StepDone
	StepFailed
)

type Event struct {
	Step  int
	State EventState
	Err   error
	Usage llm.Usage // acumulado até aqui
}

// Generator cria as conversas com o modelo do mapa.
type Generator struct {
	NewChat     func(system string, maxTokens int64) (llm.Chat, error)
	Model       string
	Commit      string
	Concurrency int
}

const systemPrompt = `Você é o cartógrafo do dante: lê o código de um repositório e escreve resumos curtos, em português,
para quem vai começar a trabalhar no projeto. Descreva só o que está no código mostrado, sem inventar. Cite código como
caminho/arquivo.ext:LINHA, usando os números de linha mostrados.`

// Run executa o plano e devolve o mapa novo. Pastas que falharem mantêm o resumo antigo,
// quando havia; o erro volta no fim.
func (g Generator) Run(ctx context.Context, p Plan, emit func(Event)) (*Map, llm.Usage, error) {
	var mu sync.Mutex
	var total llm.Usage
	add := func(u llm.Usage) llm.Usage {
		mu.Lock()
		defer mu.Unlock()
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		return total
	}
	conc := max(g.Concurrency, 1)
	summaries := map[string]string{}
	var failed []string
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, st := range p.Steps {
		if st.Folder == nil {
			continue
		}
		wg.Add(1)
		go func(i int, f Folder) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			emit(Event{Step: i, State: StepStarted, Usage: add(llm.Usage{})})
			s, u, err := g.summarizeFolder(ctx, f, p.limit)
			tot := add(u)
			mu.Lock()
			if err != nil {
				failed = append(failed, f.Dir)
			} else {
				summaries[f.Dir] = s
			}
			mu.Unlock()
			if err != nil {
				emit(Event{Step: i, State: StepFailed, Err: err, Usage: tot})
				return
			}
			emit(Event{Step: i, State: StepDone, Usage: tot})
		}(i, *st.Folder)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, total, err
	}

	m := &Map{Version: formatVersion, Commit: g.Commit, Model: g.Model, Generated: time.Now()}
	if p.old != nil {
		m.Overview = p.old.Overview
	}
	for _, f := range p.scan.Folders {
		fs := FolderSummary{Dir: f.Dir, Hash: f.Hash, Files: len(f.Files)}
		switch s, ok := summaries[f.Dir]; {
		case ok:
			fs.Summary = s
		case p.old.folder(f.Dir) != nil:
			o := p.old.folder(f.Dir)
			fs.Summary = o.Summary
			if o.Hash != f.Hash {
				fs.Hash = o.Hash // continua desatualizada: a próxima vez tenta de novo
			}
		default:
			continue
		}
		m.Folders = append(m.Folders, fs)
	}
	if last := len(p.Steps) - 1; last >= 0 && p.Steps[last].Folder == nil {
		emit(Event{Step: last, State: StepStarted, Usage: add(llm.Usage{})})
		ov, u, err := g.overview(ctx, m, p.scan)
		tot := add(u)
		if err != nil {
			emit(Event{Step: last, State: StepFailed, Err: err, Usage: tot})
			if ctx.Err() != nil {
				return nil, total, ctx.Err()
			}
			failed = append(failed, "visão geral")
		} else {
			m.Overview = ov
			emit(Event{Step: last, State: StepDone, Usage: tot})
		}
	}
	if len(failed) > 0 {
		return m, total, fmt.Errorf("não consegui resumir: %s", strings.Join(failed, ", "))
	}
	return m, total, nil
}

func (g Generator) ask(ctx context.Context, maxTokens int64, prompt string) (string, llm.Usage, error) {
	c, err := g.NewChat(systemPrompt, maxTokens)
	if err != nil {
		return "", llm.Usage{}, err
	}
	var b strings.Builder
	u, err := c.Send(ctx, prompt, func(ev llm.Event) {
		if ev.Kind == llm.EvText {
			b.WriteString(ev.Text)
		}
	})
	out := strings.TrimSpace(b.String())
	if err == nil && out == "" {
		err = fmt.Errorf("o modelo não respondeu nada")
	}
	return out, u, err
}

func numbered(f File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s ===\n", f.Path)
	for i, l := range strings.Split(strings.TrimRight(string(f.content), "\n"), "\n") {
		fmt.Fprintf(&b, "%d  %s\n", i+1, l)
	}
	return b.String()
}

func (g Generator) summarizeFolder(ctx context.Context, f Folder, limit int) (string, llm.Usage, error) {
	parts := chunks(f, limit)
	var out []string
	var total llm.Usage
	for i, files := range parts {
		var b strings.Builder
		fmt.Fprintf(&b, "Pasta: `%s` (%d arquivos)", f.Dir, len(f.Files))
		if len(parts) > 1 {
			fmt.Fprintf(&b, ", parte %d de %d: descreva só os arquivos desta parte", i+1, len(parts))
		}
		b.WriteString(".\n\nEscreva em markdown, exatamente neste formato, sem títulos nem introdução:\n\n" +
			"<uma ou duas frases sobre o papel da pasta no projeto>\n\n" +
			"- `caminho/arquivo.ext:LINHA` — o que o arquivo faz, citando os tipos e funções principais " +
			"(LINHA = onde fica o mais importante)\n\n" +
			"**Liga-se a:** pastas que esta usa ou que a usam, se der para ver pelo código (senão, omita a linha).\n\n" +
			"Uma linha por arquivo; junte os arquivos de teste numa linha só.\n\n<arquivos>\n")
		for _, fl := range files {
			b.WriteString(numbered(fl))
		}
		b.WriteString("</arquivos>")
		s, u, err := g.ask(ctx, 2000, b.String())
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		if err != nil {
			return "", total, err
		}
		if i > 0 { // nas partes seguintes, só a lista de arquivos interessa
			s = keepList(s)
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n"), total, nil
}

// keepList fica só com as linhas de lista de um resumo.
func keepList(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "- ") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func (g Generator) overview(ctx context.Context, m *Map, s Scan) (string, llm.Usage, error) {
	var b strings.Builder
	b.WriteString("Abaixo estão os resumos de cada pasta de um repositório")
	if s.readme != "" {
		b.WriteString(" e o começo do README")
	}
	b.WriteString(". Escreva exatamente estas seções, em markdown, e nada fora delas:\n\n" +
		"## Visão geral\n3 a 5 frases: o que o projeto é, para quem, e as partes principais.\n\n" +
		"## Por onde começar\nLista numerada de 3 a 5 arquivos, na ordem de leitura, cada um como " +
		"`caminho/arquivo.ext:LINHA` e o porquê.\n\n" +
		"## Fluxo principal\nPasso a passo numerado de um uso típico, do começo ao fim, citando " +
		"`caminho/arquivo.ext:LINHA` em cada etapa (use as linhas dos resumos).\n\n" +
		"## Glossário\nTermos próprios do projeto, um por linha: **termo** — significado.\n\n")
	if s.readme != "" {
		b.WriteString("<readme>\n" + s.readme + "\n</readme>\n\n")
	}
	b.WriteString("<pastas>\n")
	for _, f := range m.Folders {
		fmt.Fprintf(&b, "### %s (%d arquivos)\n%s\n\n", f.Dir, f.Files, f.Summary)
	}
	b.WriteString("</pastas>")
	return g.ask(ctx, 3000, b.String())
}

// ---- leitura: capítulos do tour e versão compacta ----

type Chapter struct {
	Title string
	Body  string
}

// sections divide o markdown pelos títulos "## ".
func sections(md string) []Chapter {
	var out []Chapter
	for _, part := range strings.Split("\n"+md, "\n## ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		title, body, _ := strings.Cut(part, "\n")
		out = append(out, Chapter{Title: strings.TrimSpace(strings.TrimLeft(title, "# ")), Body: strings.TrimSpace(body)})
	}
	return out
}

// Chapters é a ordem do tour: visão geral e por onde começar, uma pasta por capítulo, e
// depois o fluxo e o glossário.
func (m *Map) Chapters() []Chapter {
	ov := sections(m.Overview)
	head := min(2, len(ov))
	out := append([]Chapter{}, ov[:head]...)
	for _, f := range m.Folders {
		title := f.Dir
		if title == "." {
			title = "raiz do repositório"
		}
		out = append(out, Chapter{Title: title, Body: f.Summary})
	}
	return append(out, ov[head:]...)
}

// Markdown é o mapa inteiro, para ler ou exportar.
func (m *Map) Markdown() string {
	var b strings.Builder
	for _, c := range m.Chapters() {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", c.Title, c.Body)
	}
	return b.String()
}

// Compact é o mapa resumido que vai como contexto no começo de cada conversa: a visão
// geral, por onde começar e a primeira frase de cada pasta.
func (m *Map) Compact(limit int) string {
	var b strings.Builder
	ov := sections(m.Overview)
	for i := 0; i < min(2, len(ov)); i++ {
		fmt.Fprintf(&b, "## %s\n%s\n\n", ov[i].Title, ov[i].Body)
	}
	b.WriteString("## Pastas\n")
	for _, f := range m.Folders {
		first, _, _ := strings.Cut(strings.TrimSpace(f.Summary), "\n")
		fmt.Fprintf(&b, "- %s: %s\n", f.Dir, first)
	}
	s := b.String()
	if len(s) > limit {
		s = s[:limit] + "\n…"
	}
	return s
}
