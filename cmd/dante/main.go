// dante: revisor de código para o terminal, com modelos Claude, DeepSeek, Qwen ou locais.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// version é gravada na compilação: go build -ldflags "-X main.version=v0.5.0".
var version = "dev"

func main() {
	version = buildVersion(version)
	// Comandos sem interface (review, check, explain, ask, hook): para scripts, o hook do
	// Claude Code e o CI.
	if len(os.Args) > 1 && headlessCmds[os.Args[1]] {
		llm.UserAgent = "dante/" + version + " (+https://github.com/aishiteirai/dante)"
		cfg, _ := config.Load()
		os.Exit(runHeadless(cfg, os.Args[1:]))
	}
	showVersion := flag.Bool("versao", false, "mostra a versão do dante e sai")
	repoFlag := flag.String("repo", "", "abre direto este repositório (caminho, URL ou dono/repo), sem a tela inicial")
	modelFlag := flag.String("modelo", "", "usa este modelo sem perguntar, como provedor/modelo (ex.: deepseek/deepseek-chat)")
	effort := flag.String("esforco", "", "esforço do Claude: low, medium, high, xhigh, max")
	noMouse := flag.Bool("sem-mouse", false, "não captura o mouse (a seleção de texto do terminal funciona sem Shift)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uso: dante [opções]\n\nRevisor de código para o terminal.\n"+
			"Ao abrir, pergunta qual repositório revisar e qual modelo usar.\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n%s\nConfiguração de provedores e modelos: %s\n", headlessUsage, config.Path())
	}
	flag.Parse()
	// Um comando que o dante não conhece (digitado errado, ou de uma versão mais nova) não
	// abre a interface calado: diz o que houve.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "dante: comando desconhecido %q nesta versão (%s)\n\n%s", flag.Arg(0), version, headlessUsage)
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("dante", version)
		return
	}
	llm.UserAgent = "dante/" + version + " (+https://github.com/aishiteirai/dante)"

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Aviso: não consegui ler %s (%v); usando os padrões.\n", config.Path(), err)
	}
	if *effort != "" {
		cfg.Esforco = *effort
	}
	darkBg = lipgloss.HasDarkBackground()

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !*noMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(newModel(cfg, *repoFlag, *modelFlag), opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		os.Exit(1)
	}
}

// buildVersion completa a versão "dev" (sem -ldflags, como no go install @main) com o que o
// Go grava no binário: a pseudo-versão do módulo, ou o commit de um clone local.
func buildVersion(v string) string {
	if v != "dev" {
		return v
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	if mv := bi.Main.Version; mv != "" && mv != "(devel)" {
		return mv
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev == "" {
		return v
	}
	v += " (" + rev
	if dirty {
		v += ", com mudanças locais"
	}
	return strings.TrimSpace(v) + ")"
}
