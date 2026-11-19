package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
)

// dante modelos [provedor]: os modelos que dá para usar em --modelo, sem abrir a interface.
// Sem provedor, o do último modelo usado; sem ele, a lista de provedores.
func runModelos(cfg config.Config, args []string) int {
	id := firstOr(args, "")
	if id == "" && cfg.UltimoModelo != nil {
		id = cfg.UltimoModelo.Provedor
	}
	p, ok := cfg.Provider(id)
	if !ok {
		if id != "" {
			fmt.Fprintf(os.Stderr, "dante: provedor desconhecido %q\n\n", id)
		}
		fmt.Println("Provedores (dante modelos PROVEDOR lista os modelos):")
		for _, p := range cfg.Provedores {
			key := ""
			if !p.HasKey() && !p.Local {
				key = " · sem chave"
			}
			fmt.Printf("  %-12s %s%s\n", p.ID, p.Nome, key)
		}
		if id != "" {
			return 2
		}
		return 0
	}
	models := p.Modelos
	if p.Listar {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		list, err := llm.ListModels(ctx, p.LLM(), p.Filtro)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "dante: não consegui listar os modelos de %s: %v\n", p.Nome, err)
			if !p.HasKey() && !p.Local {
				fmt.Fprintln(os.Stderr, "dante: cadastre a chave na interface (m, depois c) ou defina "+p.ChaveEnv)
			}
			if len(models) == 0 {
				return 2
			}
			fmt.Fprintln(os.Stderr, "dante: mostrando a lista fixa do dante")
		} else {
			models = visibleModels(models, list)
		}
	}
	if len(models) == 0 {
		fmt.Fprintf(os.Stderr, "dante: %s não tem modelos para listar\n", p.Nome)
		return 2
	}
	last := ""
	if cfg.UltimoModelo != nil && cfg.UltimoModelo.Provedor == p.ID {
		last = cfg.UltimoModelo.Modelo
	}
	fmt.Printf("Modelos de %s (use --modelo %s/NOME):\n", p.Nome, p.ID)
	for _, m := range models {
		mark := "  "
		if m == last {
			mark = "* "
		}
		fmt.Println(mark + m)
	}
	if last != "" && strings.TrimSpace(last) != "" {
		fmt.Println("\n* o último usado")
	}
	return 0
}
