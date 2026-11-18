package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
	"github.com/charmbracelet/x/ansi"
)

func TestReferencias(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t) // a.py e b.py modificados nas mudanças locais
	os.WriteFile(filepath.Join(m.repo.Root, "c.py"), []byte("um\ndois\ntres\n"), 0o644)
	for _, args := range [][]string{{"add", "c.py"}, {"commit", "-qm", "c"}} { // c.py fica sem mudanças locais
		cmd := exec.Command("git", args...)
		cmd.Dir = m.repo.Root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	m.target = repo.WorkTree
	m.files, _ = m.repo.Files(repo.WorkTree)
	sortFiles(m.files)
	m.preview.Width, m.preview.Height = 80, 20

	txt := "Veja a.py:1 e `b.py:1`. Também c.py:2 e de novo a.py:1.\n" +
		"Não são referências: v0.5.0:3, nao/existe.go:9.\n```\na.py:1 dentro de código\n```"
	got := m.annotateRefs(txt)
	for _, want := range []string{"a.py:1 [1]", "b.py:1 [2]", "c.py:2 [3]", "de novo a.py:1 [1]", "v0.5.0:3,", "nao/existe.go:9.", "a.py:1 dentro"} {
		if !strings.Contains(got, want) {
			t.Errorf("faltou %q em:\n%s", want, got)
		}
	}
	if strings.Contains(got, "a.py:1 dentro de código [") || len(m.refs) != 3 {
		t.Fatalf("refs = %+v\n%s", m.refs, got)
	}

	// [1] é um arquivo alterado: abre o diff dele, marcando a linha.
	nm, cmd := m.openRef(1)
	m = nm.(model)
	if cmd == nil || m.pendingLine != 1 || m.files[m.fileIdx].Path != "a.py" {
		t.Fatalf("openRef(1): pending=%d arquivo=%s", m.pendingLine, m.files[m.fileIdx].Path)
	}
	msg := cmd().(previewMsg)
	nm, _ = m.Update(msg)
	m = nm.(model)
	if m.markLine != 1 || !strings.Contains(ansi.Strip(m.preview.View()), "▶") {
		t.Fatalf("linha marcada no diff: mark=%d\n%s", m.markLine, ansi.Strip(m.preview.View()))
	}

	// [3] não mudou: abre na aba Documento, na linha 2.
	nm, cmd = m.openRef(3)
	m = nm.(model)
	nm, _ = m.Update(cmd())
	m = nm.(model)
	view := ansi.Strip(m.preview.View())
	if m.vtab != vtDoc || !strings.Contains(m.docTitle, "c.py:2") || !strings.Contains(view, "▶   2  dois") {
		t.Fatalf("documento: tab=%d título=%q\n%s", m.vtab, m.docTitle, view)
	}

	// Lista (ctrl+g): um dígito abre direto.
	m.mode = modeRefs
	nm, _ = m.updateRefKeys("2")
	if nm.(model).mode != modeNormal || nm.(model).files[nm.(model).fileIdx].Path != "b.py" {
		t.Fatal("dígito deveria abrir a referência [2]")
	}
	if !strings.Contains(ansi.Strip(m.refsPopup()), "[3] c.py:2") {
		t.Fatal("lista de referências")
	}
}
