package main

import (
	"strings"
	"testing"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/mapa"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func mapModel(t *testing.T) model {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("DANTE_HOME", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.screen = scrMain
	m.width, m.height = 160, 40
	m.relayout()
	m.prov, _ = m.cfg.Provider("ollama")
	m.modelName = "qwen2.5-coder:7b"
	return m
}

func TestMapaPopupETour(t *testing.T) {
	m := mapModel(t)
	if c, _, _ := parseCommands("/mapa"); !c.mapa {
		t.Fatal("/mapa")
	}
	// Sem mapa, /tour só avisa.
	nm, _ := m.send("/tour")
	m = nm.(model)
	if !strings.Contains(m.status, "/mapa gera") {
		t.Fatalf("status = %q", m.status)
	}
	s, err := mapa.ScanRepo(m.repo)
	if err != nil {
		t.Fatal(err)
	}
	nm, _ = m.onMapScanned(mapScannedMsg{root: m.repo.Root, scan: s})
	m = nm.(model)
	if m.mode != modeMap {
		t.Fatal("deveria abrir a confirmação")
	}
	pop := ansi.Strip(m.mapPopup())
	for _, want := range []string{"gerar o mapa", "2 arquivos", "leitura única", "qwen2.5-coder:7b", "modelo da conversa"} {
		if !strings.Contains(pop, want) {
			t.Errorf("popup sem %q:\n%s", want, pop)
		}
	}

	// Mapa pronto: salvo, aba Mapa com capítulos e contexto nas conversas novas.
	m.mode = modeNormal
	m.mapGen.running = true
	mp := &mapa.Map{Version: 1, Commit: "abc1234", Model: "barato", Generated: time.Now(),
		Overview: "## Visão geral\nUm demo.\n\n## Por onde começar\n1. `a.py:1`\n\n## Glossário\n**x** — y",
		Folders:  []mapa.FolderSummary{{Dir: ".", Hash: "h", Files: 2, Summary: "Scripts soltos.\n\n- `a.py:1` — define x"}}}
	nm, _ = m.onMapDone(mapDoneMsg{m: mp, usage: llm.Usage{InputTokens: 9000, OutputTokens: 500}})
	m = nm.(model)
	if m.mapData == nil || m.mapGen != nil {
		t.Fatal("mapa não foi guardado")
	}
	if old, err := mapa.Load(mapPath(m.repo.Root)); err != nil || old.Model != "barato" {
		t.Fatalf("salvo: %v", err)
	}
	nm, _ = m.send("/tour")
	m = nm.(model)
	if m.vtab != vtMap || !strings.Contains(ansi.Strip(m.preview.View()), "Visão geral") {
		t.Fatalf("tour:\n%s", ansi.Strip(m.preview.View()))
	}
	m.focus = pDiff
	nm, _ = m.onKey(tea.KeyMsg{Type: tea.KeyRight})
	nm, _ = nm.(model).onKey(tea.KeyMsg{Type: tea.KeyRight})
	m = nm.(model)
	if out := ansi.Strip(m.preview.View()); !strings.Contains(out, "raiz do repositório") || !strings.Contains(out, "a.py:1 [") {
		t.Fatalf("capítulo 3:\n%s", out)
	}
	if !strings.Contains(m.buildSystemPrompt(), "<mapa_do_projeto") {
		t.Fatal("o mapa compacto deveria ir no prompt de sistema")
	}
}
