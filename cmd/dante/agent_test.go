package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const editA = `{"path":"a.py","old_string":"x = 2","new_string":"x = 3"}`

// runWrite chama o executor numa goroutine e devolve o primeiro changeMsg e o resultado.
func runWrite(t *testing.T, b *writeBus, m model, call llm.ToolCall, answer func(changeMsg)) (string, bool, []changeMsg) {
	t.Helper()
	ch := make(chan tea.Msg, 16)
	b.startTurn(ch)
	exec := b.wrap(m.repo, func(context.Context, llm.ToolCall) (string, bool) { return "leu", false })
	type res struct {
		out string
		bad bool
	}
	done := make(chan res, 1)
	go func() {
		out, bad := exec(context.Background(), call)
		done <- res{out, bad}
	}()
	var msgs []changeMsg
	for {
		select {
		case r := <-done:
			for len(ch) > 0 {
				msgs = append(msgs, (<-ch).(changeMsg))
			}
			return r.out, r.bad, msgs
		case msg := <-ch:
			cm := msg.(changeMsg)
			msgs = append(msgs, cm)
			if cm.reply != nil {
				answer(cm)
			}
		}
	}
}

func agentModel(t *testing.T) model {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	return m
}

func TestAgenteModoLer(t *testing.T) {
	m := agentModel(t)
	b := m.writes()
	out, bad, msgs := runWrite(t, b, m, llm.ToolCall{Name: "edit_file", Args: editA}, nil)
	if !bad || !strings.Contains(out, "Modo ler") || len(msgs) != 1 || msgs[0].card.state != chSuggested {
		t.Fatalf("ler: %q %v %+v", out, bad, msgs)
	}
	if cur, _ := os.ReadFile(filepath.Join(m.repo.Root, "a.py")); string(cur) != "x = 2\n" {
		t.Fatal("o modo ler não pode gravar")
	}
	if out, bad, _ := runWrite(t, b, m, llm.ToolCall{Name: "read_file", Args: "{}"}, nil); out != "leu" || bad {
		t.Fatal("leituras seguem para o repositório")
	}
}

func TestAgenteAprovacao(t *testing.T) {
	m := agentModel(t)
	m.setAgent(agentEditar)
	b := m.writes()
	a := filepath.Join(m.repo.Root, "a.py")

	// Recusada.
	out, _, msgs := runWrite(t, b, m, llm.ToolCall{Name: "edit_file", Args: editA}, func(c changeMsg) { c.reply <- approval{} })
	if !strings.Contains(out, "recusou") || msgs[len(msgs)-1].card.state != chDeclined {
		t.Fatalf("recusa: %q", out)
	}
	if g := b.endTurn(); len(g.changes) != 0 {
		t.Fatal("recusa não entra no desfazer")
	}

	// Aplicada com "t": a próxima da mesma resposta não pergunta.
	asked := 0
	ans := func(c changeMsg) { asked++; c.reply <- approval{ok: true, all: true} }
	ch := make(chan tea.Msg, 16)
	b.startTurn(ch)
	exec := b.wrap(m.repo, nil)
	go func() {
		for msg := range ch {
			if cm := msg.(changeMsg); cm.reply != nil {
				ans(cm)
			}
		}
	}()
	if out, bad := exec(context.Background(), llm.ToolCall{Name: "edit_file", Args: editA}); bad {
		t.Fatal(out)
	}
	if out, bad := exec(context.Background(), llm.ToolCall{Name: "write_file", Args: `{"path":"c.py","content":"z = 1\n"}`}); bad {
		t.Fatal(out)
	}
	g := b.endTurn()
	close(ch)
	if asked != 1 || len(g.changes) != 2 {
		t.Fatalf("perguntou %d vezes, aplicou %d", asked, len(g.changes))
	}
	if cur, _ := os.ReadFile(a); string(cur) != "x = 3\n" {
		t.Fatalf("a.py = %q", cur)
	}

	// ctrl+z desfaz as duas.
	m.undo = append(m.undo, g)
	nm, _ := m.undoLast()
	m = nm.(model)
	if cur, _ := os.ReadFile(a); string(cur) != "x = 2\n" {
		t.Fatalf("desfazer: a.py = %q", cur)
	}
	if _, err := os.Stat(filepath.Join(m.repo.Root, "c.py")); err == nil {
		t.Fatal("desfazer deveria apagar o arquivo criado")
	}

	// Ajustada no editor.
	out, _, msgs = runWrite(t, b, m, llm.ToolCall{Name: "edit_file", Args: editA}, func(c changeMsg) {
		c.reply <- approval{ok: true, edited: []byte("x = 42\n")}
	})
	if !strings.Contains(out, "ajustou") || msgs[len(msgs)-1].card.state != chEdited {
		t.Fatalf("ajuste: %q", out)
	}
	if cur, _ := os.ReadFile(a); string(cur) != "x = 42\n" {
		t.Fatalf("a.py = %q", cur)
	}
}

func TestAgentePermissoes(t *testing.T) {
	m := agentModel(t)
	m.setAgent(agentEditar)
	b := m.writes()
	// Apagar é negado por padrão.
	out, bad, msgs := runWrite(t, b, m, llm.ToolCall{Name: "delete_file", Args: `{"path":"b.py"}`}, nil)
	if !bad || !strings.Contains(out, "negar") || msgs[0].card.state != chDenied {
		t.Fatalf("negar: %q %+v", out, msgs)
	}
	// Permitir aplica sem perguntar.
	m.cfg.SetPermission("editar", config.PermAllow)
	b.setPerms(m.cfg)
	out, bad, msgs = runWrite(t, b, m, llm.ToolCall{Name: "edit_file", Args: editA}, func(changeMsg) { t.Fatal("não deveria perguntar") })
	if bad || !msgs[len(msgs)-1].card.auto {
		t.Fatalf("permitir: %q", out)
	}
	// Erros de plano voltam ao modelo e viram cartão de falha.
	out, bad, msgs = runWrite(t, b, m, llm.ToolCall{Name: "edit_file", Args: `{"path":".env","old_string":"a","new_string":"b"}`}, nil)
	if !bad || msgs[0].card.state != chFailed {
		t.Fatalf("sensível: %q", out)
	}
}

func TestCartaoDeMudanca(t *testing.T) {
	m := agentModel(t)
	m.setAgent(agentEditar)
	m.chatView.Width, m.chatView.Height = 90, 40
	m.modelName = "x"
	m.streaming = true
	ch := make(chan tea.Msg, 4)
	m.events = ch
	c, _ := m.repo.PlanEdit("a.py", "x = 2", "x = 3", false)
	reply := make(chan approval, 1)
	nm, _ := m.onChange(changeMsg{card: changeCard{id: 1, c: c, diff: changeDiff(c), state: chPending}, reply: reply})
	m = nm.(model)
	out := ansi.Strip(m.chatView.View())
	for _, want := range []string{"✎ EDITAR a.py", "+1 -1", "aguardando você", "-x = 2", "+x = 3", "s aplicar"} {
		if !strings.Contains(out, want) {
			t.Errorf("cartão sem %q:\n%s", want, out)
		}
	}
	if !m.docIsDiff || !strings.Contains(m.docRaw, "+++ b/a.py") {
		t.Fatalf("Visualizador: %q", m.docRaw)
	}
	nm, _ = m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = nm.(model)
	if a := <-reply; !a.ok || m.approval != nil {
		t.Fatal("s deveria aprovar")
	}
}

func TestAvisoDeEdicaoNoModoLer(t *testing.T) {
	if !asksForEdit("corrija o bug do util.py") || asksForEdit("o que esse commit faz?") {
		t.Fatal("asksForEdit")
	}
}
