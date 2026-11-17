// dante: revisor de código para o terminal, com modelos Claude, DeepSeek, Qwen ou locais.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aishiteirai/dante/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	repoFlag := flag.String("repo", "", "abre direto este repositório (caminho, URL ou dono/repo), sem a tela inicial")
	modelFlag := flag.String("modelo", "", "usa este modelo sem perguntar, como provedor/modelo (ex.: deepseek/deepseek-chat)")
	effort := flag.String("esforco", "", "esforço do Claude: low, medium, high, xhigh, max")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uso: dante [opções]\n\nRevisor de código para o terminal.\n"+
			"Ao abrir, pergunta qual repositório revisar e qual modelo usar.\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nConfiguração de provedores e modelos: %s\n", config.Path())
	}
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Aviso: não consegui ler %s (%v); usando os padrões.\n", config.Path(), err)
	}
	if *effort != "" {
		cfg.Esforco = *effort
	}
	darkBg = lipgloss.HasDarkBackground()

	p := tea.NewProgram(newModel(cfg, *repoFlag, *modelFlag), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		os.Exit(1)
	}
}
