package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFirstHunkLine(t *testing.T) {
	d := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -10,3 +42,5 @@ func x()\n"
	if n := firstHunkLine(d); n != 42 {
		t.Fatalf("linha = %d", n)
	}
	if n := firstHunkLine("nada"); n != 1 {
		t.Fatalf("sem hunk = %d", n)
	}
}

func TestRemoteURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/a/b.git": "https://github.com/a/b.git",
		"git@github.com:a/b.git":     "git@github.com:a/b.git",
		"aishiteirai/dante":          "https://github.com/aishiteirai/dante",
		"github.com/a/b":             "https://github.com/a/b",
	}
	for in, want := range cases {
		if got, ok := remoteURL(in); !ok || got != want {
			t.Errorf("remoteURL(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := remoteURL("/tmp"); ok {
		t.Error("/tmp é caminho local")
	}
	if d := cloneDir("git@github.com:dono/proj.git"); !strings.HasSuffix(d, "repos/dono-proj") {
		t.Errorf("cloneDir = %s", d)
	}
}

func TestDescribeCall(t *testing.T) {
	c := llm.ToolCall{Name: "read_file", Args: `{"path":"a.go","start_line":3,"end_line":9,"ref":"abcdef1234"}`}
	if got := describeCall(c); got != "abriu a.go:3-9 @ abcdef1" {
		t.Fatalf("got %q", got)
	}
}

func TestModelRowsEPreSelecao(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	cfg := config.Defaults()
	cfg.UltimoModelo = &config.Choice{Provedor: "ollama", Modelo: "qwen2.5-coder:7b"}
	m := newModel(cfg, "", "")
	m.enterModelScreen()
	// Antes da lista local chegar, o cursor fica no Ollama (o único pronto: procurando).
	if r := m.modelRows[m.modelIdx]; r.prov.ID != "ollama" || !r.header {
		t.Fatalf("antes da lista local: %s/%s", r.prov.ID, r.model)
	}
	nm, _ := m.Update(modelsListedMsg{prov: "ollama", models: []string{"deepseek-r1:8b", "qwen2.5-coder:7b"}})
	m = nm.(model)
	if r := m.modelRows[m.modelIdx]; r.model != "qwen2.5-coder:7b" {
		t.Fatalf("pré-seleção = %s", r.model)
	}
}

func TestTelaPrincipalRenderiza(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.screen = scrMain
	m.repo = repo.Repo{Root: "/tmp/x"}
	m.prov, _ = m.cfg.Provider("deepseek")
	m.modelName = "deepseek-chat"
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	m = nm.(model)
	m.rows = []logRow{{worktree: true}, {c: repo.Commit{Graph: "* ", SHA: "abc", Short: "abc", Subject: "primeiro"}}}
	m.dirty = 2
	m.files = []repo.FileChange{{Path: "a.go", Status: "M", Add: 3, Del: 1}}
	m.entries = []entry{{kind: eUser, text: "oi"}, {kind: eAssistant, text: "**olá**"}}
	m.refreshChat()
	v := m.View()
	for _, want := range []string{"Navegador", "Mudanças locais", "a.go", "deepseek-chat", "olá"} {
		if !strings.Contains(v, want) {
			t.Errorf("tela sem %q", want)
		}
	}
	for i, l := range strings.Split(v, "\n") {
		if w := len([]rune(stripANSI(l))); w > 150 {
			t.Fatalf("linha %d passa da largura: %d", i, w)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestFluxoDaChave(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "")
	cfg, _ := config.Load()
	m := newModel(cfg, "", "")
	m.repo = repo.Repo{Root: t.TempDir()}
	m.screen = scrModel
	m.enterModelScreen()
	for i, r := range m.modelRows { // escolhe o DeepSeek, que está sem chave
		if r.header && r.prov.ID == "deepseek" {
			m.modelIdx = i
		}
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if m.screen != scrKey || m.keyProv.ID != "deepseek" || m.keyThen == nil {
		t.Fatalf("esperava a tela de chave; tela=%d", m.screen)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-teste-123456")})
	m = nm.(model)
	if strings.Contains(m.View(), "sk-teste") {
		t.Fatal("a chave deveria ficar oculta")
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if !m.keyChecking || cmd == nil {
		t.Fatal("deveria testar a chave")
	}
	// Provedor recusa: continua na tela com erro.
	nm, _ = m.Update(keyCheckedMsg{provID: "deepseek", key: "sk-teste-123456", err: llm.ErrInvalidKey})
	m = nm.(model)
	if m.screen != scrKey || m.keyErr == "" {
		t.Fatal("deveria mostrar o erro")
	}
	// Chave aceita: salva, escolhe o modelo e abre a área de trabalho.
	nm, _ = m.Update(keyCheckedMsg{provID: "deepseek", key: "sk-teste-123456"})
	m = nm.(model)
	if m.screen != scrMain || m.modelName != "deepseek-chat" || m.prov.LLM().Key() != "sk-teste-123456" {
		t.Fatalf("depois de salvar: tela=%d modelo=%s", m.screen, m.modelName)
	}
}

func TestSugestoesDoOllama(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	cfg, _ := config.Load()
	m := newModel(cfg, "", "")
	m.screen = scrModel
	m.enterModelScreen()
	nm, _ := m.Update(modelsListedMsg{prov: "ollama", models: []string{"qwen2.5-coder:7b"}})
	m = nm.(model)
	var downloads []string
	idx := -1
	for i, r := range m.modelRows {
		if r.download {
			downloads = append(downloads, r.model)
			if r.model == "qwen3:8b" {
				idx = i
			}
		}
	}
	if len(downloads) != 4 || strings.Contains(strings.Join(downloads, ","), "qwen2.5-coder:7b") {
		t.Fatalf("sugestões = %v (a já baixada não deve aparecer)", downloads)
	}
	m.modelIdx = idx
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if m.pulling != "qwen3:8b" || cmd == nil {
		t.Fatalf("enter deveria baixar; pulling=%q", m.pulling)
	}
	m.pullCancel()
	nm, _ = m.Update(pullDoneMsg{prov: m.modelRows[idx].prov, model: "qwen3:8b"})
	m = nm.(model)
	if m.pulling != "" || m.modelWant == nil || m.modelWant.Modelo != "qwen3:8b" {
		t.Fatal("depois do download, o modelo deveria ficar pré-selecionado")
	}
}

func TestMouseFocoEArrasto(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.screen = scrMain
	m.repo = repo.Repo{Root: t.TempDir()}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	m = nm.(model)
	chat := m.rects[pChat]
	// Clique dentro do chat dá foco a ele.
	nm, _ = m.Update(tea.MouseMsg{X: chat.x + 5, Y: chat.y + 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = nm.(model)
	if m.focus != pChat {
		t.Fatalf("foco = %s", paneNames[m.focus])
	}
	// Arrastar a borda entre o Navegador e o Visualizador 10 colunas para a direita.
	before := m.rects[pDiff].w
	border := m.rects[pDiff].x
	nm, _ = m.Update(tea.MouseMsg{X: border, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = nm.(model)
	if m.drag == nil || !m.drag.vertical {
		t.Fatal("deveria começar a arrastar a borda")
	}
	nm, _ = m.Update(tea.MouseMsg{X: border + 10, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = nm.(model)
	nm, _ = m.Update(tea.MouseMsg{X: border + 10, Y: 5, Action: tea.MouseActionRelease})
	m = nm.(model)
	if got := m.rects[pDiff].w; got < before-11 || got > before-9 {
		t.Fatalf("largura do Visualizador %d -> %d, esperava ~%d", before, got, before-10)
	}
	if m.rects[pDiff].w != m.rects[pChat].w || m.rects[pDiff].w+m.rects[pLog].w != 150 {
		t.Fatal("as colunas não somam a largura da tela")
	}
	// A borda entre o Visualizador e o chat sobe 4 linhas.
	d := m.rects[pDiff]
	h := d.h
	nm, _ = m.Update(tea.MouseMsg{X: d.x + 20, Y: d.y + d.h - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = nm.(model)
	nm, _ = m.Update(tea.MouseMsg{X: d.x + 20, Y: d.y + d.h - 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = nm.(model)
	nm, _ = m.Update(tea.MouseMsg{X: d.x + 20, Y: d.y + d.h - 5, Action: tea.MouseActionRelease})
	m = nm.(model)
	if got := m.rects[pDiff].h; got != h-4 {
		t.Fatalf("altura do Visualizador %d -> %d", h, got)
	}
}

func TestDescribeReadFiles(t *testing.T) {
	c := llm.ToolCall{Name: "read_files", Args: `{"files":[{"path":"a.go"},{"path":"b.go","start_line":3}]}`}
	if got := describeCall(c); got != "abriu 2 arquivos: a.go, b.go" {
		t.Fatalf("got %q", got)
	}
}

func gitRepo(t *testing.T) repo.Repo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	run("git", "init", "-q")
	run("git", "config", "user.email", "t@example.com")
	run("git", "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.py"), []byte("y = 1\n"), 0o644)
	run("git", "add", ".")
	run("git", "commit", "-qm", "inicial")
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.py"), []byte("y = 2\n"), 0o644)
	root, _ := repo.Root(dir)
	return repo.Repo{Root: root}
}

func TestDiffNaMensagem(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.target = repo.WorkTree

	first := m.diffBlock("")
	if !strings.Contains(first, "<diff do alvo>") || !strings.Contains(first, "+x = 2") || !strings.Contains(first, "+y = 2") {
		t.Fatalf("primeira mensagem sem o diff:\n%s", first)
	}
	// Enquanto a resposta não termina bem, o diff não conta como enviado.
	if again := m.diffBlock(""); again == "" {
		t.Fatal("sem resposta concluída, o diff deveria ir de novo")
	}
	nm, _ := m.onChatDone(chatDoneMsg{})
	m = nm.(model)
	if again := m.diffBlock(""); again != "" {
		t.Fatalf("diff repetido na mesma conversa:\n%s", again)
	}
	if file := m.diffBlock("a.py"); file != "" {
		t.Fatal("o diff do alvo inteiro já cobre o arquivo")
	}
	// Diff grande: os arquivos vão para a lista do que ficou de fora.
	m.resetChat()
	m.prov.Contexto = 20 // ~15 bytes de limite
	if big := m.diffBlock(""); !strings.Contains(big, "a.py (+1 −1, não coube aqui)") || strings.Contains(big, "+x = 2") {
		t.Fatalf("diff grande deveria virar lista:\n%s", big)
	}
	// Pedido de um arquivo só leva o diff dele.
	m.resetChat()
	m.prov.Contexto = 0
	if one := m.diffBlock("b.py"); !strings.Contains(one, "<diff de b.py>") || strings.Contains(one, "+x = 2") {
		t.Fatalf("diff do arquivo:\n%s", one)
	}
}

func TestGastoDaResposta(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.chatView.Width = 80
	nm, _ := m.onChatDone(chatDoneMsg{usage: llm.Usage{InputTokens: 55000, OutputTokens: 3200, CachedTokens: 40000, Rounds: 2}})
	m = nm.(model)
	if got := m.entries[0]; got.kind != eUsage || got.text != "58,2 mil tokens nesta resposta · 2 rodadas (entrada 55,0 mil, 40,0 mil do cache · saída 3,2 mil)" {
		t.Fatalf("entrada = %+v", got)
	}
	// Com ~27 mil tokens por requisição, a dica de ctrl+n aparece (uma vez por conversa).
	if got := m.entries[len(m.entries)-1]; got.kind != eNotice || !strings.Contains(got.text, "ctrl+n") {
		t.Fatalf("dica = %+v", got)
	}
	nm, _ = m.onChatDone(chatDoneMsg{usage: llm.Usage{InputTokens: 55000, Rounds: 2}})
	if n := len(nm.(model).entries); n != 3 {
		t.Fatalf("a dica deveria aparecer uma vez só: %d entradas", n)
	}
	if humanTokens(950) != "950" || humanTokens(1_250_000) != "1,25 mi" {
		t.Fatal("humanTokens")
	}
}

func TestContextoPadrao(t *testing.T) {
	m := model{}
	m.prov = config.Provider{Tipo: llm.KindOpenAI} // ex.: Baidu acrescentado sem "contexto"
	if got := m.effectiveContext(); got != defaultContextTokens {
		t.Fatalf("sem contexto: %d", got)
	}
	m.prov.Contexto = 8000
	if m.effectiveContext() != 8000 {
		t.Fatal("contexto configurado deveria valer")
	}
	m.prov = config.Provider{Tipo: llm.KindAnthropic}
	if m.effectiveContext() != 0 {
		t.Fatal("no Claude o servidor cuida do contexto")
	}
}

func TestEditorCommand(t *testing.T) {
	cases := []struct {
		editor, goos string
		want         string
	}{
		{"", "linux", "vi +7 f.go"},
		{"", "windows", "notepad f.go"},
		{"code --wait", "windows", "code --wait -g f.go:7"},
		{`C:\Tools\nvim.exe`, "windows", `C:\Tools\nvim.exe +7 f.go`},
		{"notepad++.exe", "windows", "notepad++.exe f.go"},
	}
	for _, c := range cases {
		if got := strings.Join(editorCommand(c.editor, c.goos, "f.go", 7), " "); got != c.want {
			t.Errorf("editorCommand(%q, %s) = %q, queria %q", c.editor, c.goos, got, c.want)
		}
	}
}

func TestDiffJaEnviadoNaoRepete(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.target = repo.WorkTree
	if b := m.diffBlock(""); !strings.Contains(b, "+x = 2") {
		t.Fatalf("pacote: %s", b)
	}
	target := repo.WorkTree
	exec := withPacked(m.packed, &target, func(context.Context, llm.ToolCall) (string, bool) { return "diff de novo", false })
	call := func(args string) string {
		out, _ := exec(context.Background(), llm.ToolCall{Name: "git_diff", Args: args})
		return out
	}
	if out := call(`{"path":"a.py"}`); !strings.Contains(out, "já veio") {
		t.Fatalf("git_diff repetido: %q", out)
	}
	if out := call(`{"path":"a.py","context_lines":10}`); out != "diff de novo" {
		t.Fatal("com mais contexto, o diff é gerado")
	}
	// A resposta falhou: a mensagem sai do histórico, e o diff volta a ser pedido.
	nm, _ := m.onChatDone(chatDoneMsg{err: fmt.Errorf("falhou")})
	m = nm.(model)
	if out := call(`{"path":"a.py"}`); out != "diff de novo" {
		t.Fatal("depois de uma falha, o diff precisa ser gerado de novo")
	}
}

func TestTelaDeModelosEnxuta(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "sk-x")
	t.Setenv("GEMINI_API_KEY", "g-x")
	cfg, _ := config.Load()
	m := newModel(cfg, "", "")
	m.width, m.height = 120, 40
	m.screen = scrModel
	m.enterModelScreen()
	nm, _ := m.Update(modelsListedMsg{prov: "ollama", err: fmt.Errorf("desligado")})
	nm, _ = nm.(model).Update(modelsListedMsg{prov: "lmstudio", err: fmt.Errorf("desligado")})
	m = nm.(model)
	names := func() (out []string) {
		for _, r := range m.modelRows {
			switch {
			case r.label != "":
				out = append(out, "#")
			case r.header:
				out = append(out, r.prov.ID)
			default:
				out = append(out, "  "+r.model)
			}
		}
		return
	}
	got := strings.Join(names(), ",")
	// Prontos primeiro, só o primeiro aberto; os outros numa linha cada.
	if !strings.HasPrefix(got, "#,deepseek,  deepseek-chat,  deepseek-reasoner,gemini,#,claude") || strings.Contains(got, "claude-opus") {
		t.Fatalf("linhas = %s", got)
	}
	// Descer até o Gemini abre o Gemini e fecha o DeepSeek.
	for i := 0; i < 3; i++ {
		nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = nm.(model)
	}
	if r := m.modelRows[m.modelIdx]; !r.header || r.prov.ID != "gemini" || m.modelExp != "gemini" || strings.Contains(strings.Join(names(), ","), "deepseek-chat") {
		t.Fatalf("sanfona: cursor em %s, aberto %s, linhas %v", r.prov.ID, m.modelExp, names())
	}
	// Busca em todos os provedores, inclusive os sem chave.
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/")}, {Type: tea.KeyRunes, Runes: []rune("opus")}} {
		nm, _ = m.Update(k)
		m = nm.(model)
	}
	if len(m.modelRows) != 1 || m.modelRows[0].model != "claude-opus-5-5" || !strings.Contains(stripANSI(m.View()), "sem chave") {
		t.Fatalf("busca: %v", names())
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(model)
	if m.modelFilter != "" || m.modelFiltering || len(m.modelRows) < 5 {
		t.Fatal("esc deveria limpar a busca")
	}
}
