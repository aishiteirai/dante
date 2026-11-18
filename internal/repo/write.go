package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
)

// Escrita de arquivos pelo agente. Cada mudança é primeiro PLANEJADA (sem tocar no disco),
// para o desenvolvedor aprovar, e só depois APLICADA. Aplicar confere que o arquivo não mudou
// desde o plano; desfazer confere que ele continua como o agente deixou. Nunca escreve em
// arquivos sensíveis, na pasta .git ou fora do repositório.

type WriteOp string

const (
	OpEdit   WriteOp = "editar"
	OpCreate WriteOp = "criar"
	OpDelete WriteOp = "apagar"
)

// Change é uma mudança planejada num arquivo.
type Change struct {
	Op     WriteOp
	Path   string // relativo à raiz, com "/"
	Before []byte // conteúdo antes (nil se o arquivo não existia)
	After  []byte // conteúdo depois (nil ao apagar)
	Mode   os.FileMode
}

// WriteTools são as ferramentas de escrita oferecidas ao modelo.
func WriteTools() []llm.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return []llm.Tool{
		{Name: "edit_file", Description: "Só com permissão editar. Troca um trecho exato do arquivo no disco: `old_string` " +
			"precisa aparecer uma vez (copie com a indentação), ou use replace_all.",
			Params: map[string]any{"type": "object", "required": []string{"path", "old_string", "new_string"},
				"properties": map[string]any{
					"path":        str("Arquivo."),
					"old_string":  str("Trecho atual."),
					"new_string":  str("Trecho novo."),
					"replace_all": map[string]any{"type": "boolean"},
				}}},
		{Name: "write_file", Description: "Só com permissão editar. Cria um arquivo novo (para mudar um existente, edit_file).",
			Params: map[string]any{"type": "object", "required": []string{"path", "content"},
				"properties": map[string]any{"path": str("Arquivo novo."), "content": str("Conteúdo completo.")}}},
		{Name: "delete_file", Description: "Só com permissão editar. Apaga um arquivo.",
			Params: map[string]any{"type": "object", "required": []string{"path"},
				"properties": map[string]any{"path": str("Arquivo.")}}},
	}
}

// writablePath confere que o agente pode escrever no caminho.
func (r Repo) writablePath(p string) (string, string, error) {
	clean, err := r.safePath(p)
	if err != nil {
		return "", "", err
	}
	if clean == "." || clean == "" {
		return "", "", fmt.Errorf("informe um arquivo")
	}
	if isGitDir(clean) {
		return "", "", fmt.Errorf("o agente não mexe na pasta .git")
	}
	if r.Blocked(clean) {
		return "", "", blockedErr(clean)
	}
	full := filepath.Join(r.Root, filepath.FromSlash(clean))
	// O diretório (e o próprio arquivo, se existir) não pode levar para fora por link simbólico.
	rootReal, _ := filepath.EvalSymlinks(r.Root)
	for _, c := range []string{full, filepath.Dir(full)} {
		if real, err := filepath.EvalSymlinks(c); err == nil {
			if real != rootReal && !strings.HasPrefix(real, rootReal+string(os.PathSeparator)) {
				return "", "", fmt.Errorf("caminho fora do repositório: %s", p)
			}
		}
	}
	return clean, full, nil
}

func isGitDir(p string) bool { return p == ".git" || strings.HasPrefix(p, ".git/") }

func (r Repo) PlanEdit(path, oldS, newS string, all bool) (Change, error) {
	clean, full, err := r.writablePath(path)
	if err != nil {
		return Change{}, err
	}
	st, err := os.Stat(full)
	if err != nil {
		return Change{}, fmt.Errorf("arquivo não encontrado: %s (para criar, use write_file)", clean)
	}
	before, err := os.ReadFile(full)
	if err != nil {
		return Change{}, err
	}
	if oldS == "" {
		return Change{}, fmt.Errorf("`old_string` vazio: copie o trecho a trocar")
	}
	if oldS == newS {
		return Change{}, fmt.Errorf("`old_string` e `new_string` são iguais")
	}
	n := strings.Count(string(before), oldS)
	switch {
	case n == 0:
		return Change{}, fmt.Errorf("o trecho não aparece em %s; leia o arquivo de novo (read_file sem ref) e copie exatamente", clean)
	case n > 1 && !all:
		return Change{}, fmt.Errorf("o trecho aparece %d vezes em %s; inclua mais linhas para torná-lo único, ou use replace_all", n, clean)
	}
	after := strings.Replace(string(before), oldS, newS, -1)
	if !all {
		after = strings.Replace(string(before), oldS, newS, 1)
	}
	return Change{Op: OpEdit, Path: clean, Before: before, After: []byte(after), Mode: st.Mode().Perm()}, nil
}

func (r Repo) PlanCreate(path, content string) (Change, error) {
	clean, full, err := r.writablePath(path)
	if err != nil {
		return Change{}, err
	}
	if _, err := os.Lstat(full); err == nil {
		return Change{}, fmt.Errorf("%s já existe; para mudá-lo, use edit_file", clean)
	}
	return Change{Op: OpCreate, Path: clean, After: []byte(content), Mode: 0o644}, nil
}

func (r Repo) PlanDelete(path string) (Change, error) {
	clean, full, err := r.writablePath(path)
	if err != nil {
		return Change{}, err
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		return Change{}, fmt.Errorf("arquivo não encontrado: %s", clean)
	}
	before, err := os.ReadFile(full)
	if err != nil {
		return Change{}, err
	}
	return Change{Op: OpDelete, Path: clean, Before: before, Mode: st.Mode().Perm()}, nil
}

// current lê o arquivo agora (nil se não existe).
func (r Repo) current(c Change) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(c.Path)))
	if err != nil {
		return nil, false
	}
	return b, true
}

// Apply grava a mudança, se o arquivo continua como no plano.
func (r Repo) Apply(c Change) error {
	if _, _, err := r.writablePath(c.Path); err != nil {
		return err
	}
	cur, exists := r.current(c)
	switch {
	case c.Before == nil && exists:
		return fmt.Errorf("%s passou a existir desde a proposta; nada foi feito", c.Path)
	case c.Before != nil && (!exists || !bytes.Equal(cur, c.Before)):
		return fmt.Errorf("%s mudou desde a proposta; nada foi feito", c.Path)
	}
	full := filepath.Join(r.Root, filepath.FromSlash(c.Path))
	if c.After == nil {
		return os.Remove(full)
	}
	return writeAtomic(full, c.After, c.Mode)
}

// Revert desfaz a mudança, se o arquivo continua como o agente deixou.
func (r Repo) Revert(c Change) error {
	cur, exists := r.current(c)
	switch {
	case c.After == nil && exists:
		return fmt.Errorf("%s foi recriado depois; não desfiz", c.Path)
	case c.After != nil && (!exists || !bytes.Equal(cur, c.After)):
		return fmt.Errorf("%s mudou depois da mudança do agente; não desfiz", c.Path)
	}
	full := filepath.Join(r.Root, filepath.FromSlash(c.Path))
	if c.Before == nil {
		return os.Remove(full)
	}
	return writeAtomic(full, c.Before, c.Mode)
}

func writeAtomic(full string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".dante-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), full)
}

// Diff devolve o diff unificado da mudança (pelo git, entre arquivos temporários).
func (c Change) Diff() string {
	dir, err := os.MkdirTemp("", "dante-diff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	a, b := filepath.Join(dir, "antes"), filepath.Join(dir, "depois")
	if c.Before != nil {
		_ = os.WriteFile(a, c.Before, 0o600)
	} else {
		a = os.DevNull
	}
	if c.After != nil {
		_ = os.WriteFile(b, c.After, 0o600)
	} else {
		b = os.DevNull
	}
	out, _ := exec.Command("git", "diff", "--no-index", "--no-color", "-U3", a, b).Output()
	return string(out)
}
