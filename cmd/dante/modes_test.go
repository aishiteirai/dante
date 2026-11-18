package main

import (
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
)

func TestParseCommands(t *testing.T) {
	c, rest, unk := parseCommands("/explicar /aprendiz como funciona o chat?")
	if c.task == nil || *c.task != taskExplicar || c.level == nil || *c.level != levelAprendiz || rest != "como funciona o chat?" || unk != "" {
		t.Fatalf("got %+v %q %q", c, rest, unk)
	}
	if c, rest, _ = parseCommands("/rápido"); c.task != nil || c.level == nil || *c.level != levelRapido || rest != "" {
		t.Fatalf("/rápido sozinho: %+v %q", c, rest)
	}
	if _, _, unk = parseCommands("/foo tudo bem"); unk != "/foo" {
		t.Fatalf("desconhecido = %q", unk)
	}
	if c, rest, _ = parseCommands("sem comando /explicar no meio"); c.task != nil || c.level != nil || rest != "sem comando /explicar no meio" {
		t.Fatal("comando no meio do texto não conta")
	}
	if c, rest, _ = parseCommands("/editar corrija o bug"); c.agent == nil || *c.agent != agentEditar || rest != "corrija o bug" {
		t.Fatalf("/editar: %+v %q", c, rest)
	}
	if c, _, _ = parseCommands("/permissões"); !c.perms {
		t.Fatal("/permissões")
	}
}

func TestDetectMode(t *testing.T) {
	cases := []struct {
		text  string
		task  string // "" = sem sinal
		level string
	}{
		{"me explica como se eu fosse um estagiário o que esse commit faz", "explicar", "aprendiz"},
		{"seja pontual: tem algum problema nesse commit?", "revisar", "rápido"},
		{"Como funciona o painel de commits?", "explicar", ""},
		{"revise util.py", "revisar", ""},
		{"Explique em detalhes a poda de contexto", "explicar", "normal"},
		{"obrigado!", "", ""},
		{"do que se trata esse ultimo commit", "explicar", ""},
		{"o que mudou nesse commit?", "explicar", ""},
	}
	for _, c := range cases {
		tk, lv := detectMode(c.text)
		gotT, gotL := "", ""
		if tk != nil {
			gotT = taskNames[*tk]
		}
		if lv != nil {
			gotL = levelNames[*lv]
		}
		if gotT != c.task || gotL != c.level {
			t.Errorf("%q → %q/%q, queria %q/%q", c.text, gotT, gotL, c.task, c.level)
		}
	}
}

func TestModoNaMensagem(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.prov, _ = m.cfg.Provider("ollama")
	m.modelName = "x"

	// Só o comando: troca o modo e não envia nada.
	nm, _ := m.send("/explicar /aprendiz")
	m = nm.(model)
	if m.streaming || m.task != taskExplicar || m.level != levelAprendiz {
		t.Fatalf("modo = %s, enviando=%v", modeLabel(m.task, m.level), m.streaming)
	}
	if last := m.entries[len(m.entries)-1]; last.kind != eNotice || last.text != "modo: explicar · aprendiz" {
		t.Fatalf("aviso = %+v", last)
	}
	// Detecção: "seja pontual" + "tem algum problema" vira revisar · rápido, com aviso.
	m.chat = nil
	nm, _ = m.send("seja pontual: tem algum problema?")
	m = nm.(model)
	if m.task != taskRevisar || m.level != levelRapido {
		t.Fatalf("detecção = %s", modeLabel(m.task, m.level))
	}
	found := false
	for _, e := range m.entries {
		if e.kind == eNotice && strings.Contains(e.text, "detectado") && strings.Contains(e.text, "revisar · rápido") {
			found = true
		}
	}
	if !found {
		t.Fatal("faltou o aviso de modo detectado")
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.streaming = false
	// Comando desconhecido: erro, nada muda.
	nm, _ = m.send("/xyz oi")
	if !strings.Contains(nm.(model).status, "comando desconhecido") {
		t.Fatal("deveria avisar do comando desconhecido")
	}
	// O bloco <modo> descreve a tarefa e o formato.
	b := modeBlock(taskExplicar, levelAprendiz, "")
	if !strings.Contains(b, "Tarefa: explicar") || !strings.Contains(b, "Palavras novas") {
		t.Fatalf("modeBlock = %s", b)
	}
	if s := commandSuggestions("/ex"); len(s) != 1 || s[0].name != "/explicar" {
		t.Fatalf("sugestões = %+v", s)
	}
}
