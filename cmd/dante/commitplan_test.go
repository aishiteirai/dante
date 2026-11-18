package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
)

func TestPlanoDeCommits(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t) // a.py e b.py alterados
	os.WriteFile(filepath.Join(m.repo.Root, "c.md"), []byte("doc\n"), 0o644)
	m.target = repo.WorkTree
	m.files, _ = m.repo.Files(repo.WorkTree)

	resposta := "Aqui está:\n```json\n" + `{"commits":[
		{"mensagem":"fix(a): corrige x","corpo":"x estava errado","arquivos":["a.py","nada.go"]},
		{"mensagem":"feat(b): muda y","corpo":"","arquivos":["b.py","a.py"]}]}` + "\n```"
	p, err := parsePlan(resposta, m.files)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.commits) != 2 || strings.Join(p.commits[1].Arquivos, ",") != "b.py" || strings.Join(p.missing, ",") != "c.md" || len(p.notes) != 2 {
		t.Fatalf("plano = %+v / faltando %v / avisos %v", p.commits, p.missing, p.notes)
	}
	if _, err := parsePlan("não sei", m.files); err == nil {
		t.Fatal("resposta sem JSON deveria falhar")
	}

	// Commitar todos, em ordem: c.md fica de fora, pendente.
	m.plan, m.mode = p, modeCommits
	nm, _ := m.updateCommitKeys("a")
	m = nm.(model)
	if m.plan != nil || m.mode != modeNormal {
		t.Fatal("depois de commitar tudo, o plano fecha")
	}
	if subs := m.repo.RecentSubjects(2); strings.Join(subs, " | ") != "feat(b): muda y | fix(a): corrige x" {
		t.Fatalf("commits = %v", subs)
	}
	if wt, _ := m.repo.Files(repo.WorkTree); len(wt) != 1 || wt[0].Path != "c.md" {
		t.Fatalf("pendente = %+v", wt)
	}
}
