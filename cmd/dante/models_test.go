package main

import (
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
)

func TestModelosVisiveis(t *testing.T) {
	// A conta não tem mais o gemini-2.5-pro: ele some; os da lista fixa que existem vêm antes.
	got := visibleModels([]string{"gemini-2.5-pro", "gemini-2.5-flash"}, []string{"gemini-3.1-pro-preview", "gemini-2.5-flash"})
	if strings.Join(got, ",") != "gemini-2.5-flash,gemini-3.1-pro-preview" {
		t.Fatalf("modelos = %v", got)
	}
	if got := visibleModels([]string{"a"}, nil); len(got) != 1 {
		t.Fatal("sem lista, vale a fixa")
	}
}

func TestMapaUsaFlashQueExiste(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	p, _ := m.cfg.Provider("gemini")
	m.listed["gemini"] = []string{"gemini-3.1-pro-preview", "gemini-3-flash-preview"}
	if got := m.cheapModel(p); got != "gemini-3-flash-preview" {
		t.Fatalf("modelo barato = %s", got)
	}
	m.listed["gemini"] = []string{"gemini-2.5-flash"}
	if got := m.cheapModel(p); got != "gemini-2.5-flash" {
		t.Fatalf("modelo barato = %s", got)
	}
}
