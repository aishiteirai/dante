package tech

import (
	"regexp"
	"strings"
)

var majorSuffix = regexp.MustCompile(`/v[0-9]+$`)

type entry struct{ name, kind, what string }

// catalog descreve as tecnologias mais comuns, numa frase, para quem está chegando. A chave
// é o pacote como aparece no manifesto; um "/" no fim casa com tudo que começa assim.
var catalog = map[string]entry{
	// linguagens
	"go":     {"Go", "linguagem", "linguagem compilada do Google, com tipos estáticos, goroutines para concorrência e um binário único no fim"},
	"python": {"Python", "linguagem", "linguagem interpretada de tipagem dinâmica, comum em scripts, web, dados e IA"},
	"rust":   {"Rust", "linguagem", "linguagem compilada focada em segurança de memória sem coletor de lixo (posse e empréstimo)"},
	"ruby":   {"Ruby", "linguagem", "linguagem interpretada e expressiva, conhecida pelo Rails"},

	// Go: interface de terminal
	"github.com/charmbracelet/bubbletea": {"Bubble Tea", "interface", "framework de interfaces de terminal no modelo Elm: um estado (Model), Update recebe mensagens e devolve o novo estado, View desenha"},
	"github.com/charmbracelet/bubbles":   {"Bubbles", "interface", "componentes prontos para o Bubble Tea: campo de texto, lista, viewport com rolagem, spinner"},
	"github.com/charmbracelet/lipgloss":  {"Lip Gloss", "interface", "estilos para o terminal (cores, bordas, margens) e composição de blocos de texto, como um CSS"},
	"github.com/charmbracelet/glamour":   {"Glamour", "interface", "renderiza markdown com cores no terminal"},
	"github.com/charmbracelet/x/ansi":    {"x/ansi", "interface", "medir, cortar e quebrar texto com códigos de cor ANSI sem estragar a largura"},
	"github.com/muesli/termenv":          {"termenv", "interface", "detecta o que o terminal suporta (cores, fundo escuro) e envia sequências como a área de transferência OSC 52"},
	"github.com/spf13/cobra":             {"Cobra", "ferramenta", "estrutura de linha de comando com subcomandos, opções e ajuda gerada"},
	"github.com/urfave/cli/":             {"urfave/cli", "ferramenta", "estrutura de linha de comando com subcomandos e opções"},
	// Go: web, dados, testes, IA
	"github.com/gin-gonic/gin":               {"Gin", "http", "framework web em Go: rotas, middlewares e respostas JSON"},
	"github.com/labstack/echo/":              {"Echo", "http", "framework web em Go: rotas, middlewares e respostas JSON"},
	"github.com/go-chi/chi/":                 {"chi", "http", "roteador HTTP leve, compatível com a biblioteca padrão net/http"},
	"github.com/gofiber/fiber/":              {"Fiber", "http", "framework web em Go inspirado no Express"},
	"gorm.io/gorm":                           {"GORM", "banco", "ORM para Go: mapeia structs para tabelas"},
	"github.com/jackc/pgx/":                  {"pgx", "banco", "driver do PostgreSQL para Go"},
	"github.com/mattn/go-sqlite3":            {"go-sqlite3", "banco", "driver do SQLite para Go (usa cgo)"},
	"modernc.org/sqlite":                     {"sqlite (modernc)", "banco", "SQLite em Go puro, sem cgo"},
	"github.com/redis/go-redis/":             {"go-redis", "banco", "cliente do Redis para Go"},
	"github.com/stretchr/testify":            {"testify", "teste", "asserções (assert, require) e mocks para os testes em Go"},
	"github.com/anthropics/anthropic-sdk-go": {"SDK da Anthropic (Go)", "ia", "cliente oficial da API do Claude: mensagens, streaming e ferramentas"},
	"github.com/openai/openai-go":            {"SDK da OpenAI (Go)", "ia", "cliente oficial da API da OpenAI"},
	"github.com/sashabaranov/go-openai":      {"go-openai", "ia", "cliente não oficial da API da OpenAI e compatíveis"},
	"google.golang.org/grpc":                 {"gRPC", "http", "chamadas remotas com contratos em Protocol Buffers"},
	"github.com/spf13/viper":                 {"Viper", "ferramenta", "configuração vinda de arquivos, variáveis de ambiente e opções"},

	// JavaScript e TypeScript
	"react":                  {"React", "framework", "biblioteca de interfaces com componentes e estado; a tela é uma função do estado"},
	"react-dom":              {"React DOM", "framework", "liga o React ao navegador"},
	"next":                   {"Next.js", "framework", "framework React com rotas por pasta, renderização no servidor e API no mesmo projeto"},
	"vue":                    {"Vue", "framework", "framework de interfaces reativas com componentes de arquivo único"},
	"nuxt":                   {"Nuxt", "framework", "framework Vue com rotas por pasta e renderização no servidor"},
	"svelte":                 {"Svelte", "framework", "framework que compila os componentes para JavaScript simples"},
	"@sveltejs/kit":          {"SvelteKit", "framework", "framework de aplicações com Svelte: rotas, servidor e build"},
	"@angular/core":          {"Angular", "framework", "framework completo de aplicações web, com injeção de dependência e TypeScript"},
	"express":                {"Express", "http", "servidor web minimalista para Node.js: rotas e middlewares"},
	"fastify":                {"Fastify", "http", "servidor web para Node.js focado em desempenho e validação por esquema"},
	"@nestjs/core":           {"NestJS", "http", "framework de back-end em TypeScript com módulos, decorators e injeção de dependência"},
	"hono":                   {"Hono", "http", "servidor web leve que roda em Node, Bun, Deno e na borda"},
	"typescript":             {"TypeScript", "linguagem", "JavaScript com tipos estáticos, checados pelo tsc antes de rodar"},
	"vite":                   {"Vite", "ferramenta", "servidor de desenvolvimento e empacotador rápido para front-end"},
	"webpack":                {"webpack", "ferramenta", "empacotador de módulos para front-end"},
	"tailwindcss":            {"Tailwind CSS", "interface", "CSS por classes utilitárias direto no HTML (p-4, text-sm, flex)"},
	"zod":                    {"Zod", "biblioteca", "validação de dados por esquema, com os tipos do TypeScript inferidos do esquema"},
	"axios":                  {"Axios", "http", "cliente HTTP para o navegador e o Node"},
	"@tanstack/react-query":  {"TanStack Query", "framework", "busca, cache e sincronização de dados do servidor no React"},
	"redux":                  {"Redux", "framework", "estado global num único store, alterado por ações"},
	"@reduxjs/toolkit":       {"Redux Toolkit", "framework", "jeito oficial e mais curto de usar o Redux"},
	"zustand":                {"Zustand", "framework", "estado global simples para React, com hooks"},
	"prisma":                 {"Prisma", "banco", "ORM com esquema próprio, migrações e cliente tipado"},
	"@prisma/client":         {"Prisma Client", "banco", "cliente tipado gerado a partir do esquema do Prisma"},
	"drizzle-orm":            {"Drizzle", "banco", "ORM em TypeScript com consultas parecidas com SQL"},
	"mongoose":               {"Mongoose", "banco", "modelos e validação para o MongoDB"},
	"pg":                     {"node-postgres", "banco", "driver do PostgreSQL para Node"},
	"jest":                   {"Jest", "teste", "executor de testes para JavaScript, com asserções e mocks"},
	"vitest":                 {"Vitest", "teste", "executor de testes compatível com o Jest, integrado ao Vite"},
	"@playwright/test":       {"Playwright", "teste", "testes de ponta a ponta num navegador de verdade"},
	"cypress":                {"Cypress", "teste", "testes de ponta a ponta no navegador"},
	"@testing-library/react": {"Testing Library", "teste", "testa componentes como o usuário os vê (texto, papéis), não pelos detalhes internos"},
	"eslint":                 {"ESLint", "ferramenta", "linter de JavaScript e TypeScript"},
	"@biomejs/biome":         {"Biome", "ferramenta", "formatador e linter rápido para JavaScript e TypeScript"},
	"prettier":               {"Prettier", "ferramenta", "formatador de código"},
	"openai":                 {"SDK da OpenAI", "ia", "cliente oficial da API da OpenAI"},
	"@anthropic-ai/sdk":      {"SDK da Anthropic", "ia", "cliente oficial da API do Claude"},
	"ai":                     {"Vercel AI SDK", "ia", "funções para conversar com modelos de vários provedores, com streaming"},
	"socket.io":              {"Socket.IO", "http", "comunicação em tempo real entre navegador e servidor"},

	// Python
	"django":           {"Django", "framework", "framework web completo: ORM, administração, rotas e templates"},
	"flask":            {"Flask", "http", "microframework web: rotas e respostas, o resto por extensões"},
	"fastapi":          {"FastAPI", "http", "framework de APIs com validação pelos tipos do Python (Pydantic) e documentação automática"},
	"pydantic":         {"Pydantic", "biblioteca", "validação de dados a partir de classes com anotações de tipo"},
	"sqlalchemy":       {"SQLAlchemy", "banco", "ORM e construtor de consultas SQL"},
	"requests":         {"Requests", "http", "cliente HTTP simples"},
	"httpx":            {"HTTPX", "http", "cliente HTTP com suporte a async"},
	"pytest":           {"pytest", "teste", "executor de testes: funções test_* com assert simples e fixtures"},
	"numpy":            {"NumPy", "biblioteca", "arrays numéricos e operações vetorizadas"},
	"pandas":           {"pandas", "biblioteca", "tabelas (DataFrame) para ler, filtrar e agregar dados"},
	"scikit-learn":     {"scikit-learn", "ia", "aprendizado de máquina clássico: modelos, pré-processamento e métricas"},
	"torch":            {"PyTorch", "ia", "redes neurais com tensores e diferenciação automática"},
	"transformers":     {"Transformers", "ia", "modelos pré-treinados da Hugging Face"},
	"anthropic":        {"SDK da Anthropic (Python)", "ia", "cliente oficial da API do Claude"},
	"claude-agent-sdk": {"Claude Agent SDK", "ia", "monta agentes com o Claude: ferramentas, permissões e o laço de execução"},
	"langchain":        {"LangChain", "ia", "componentes para encadear chamadas a modelos, ferramentas e buscas"},
	"celery":           {"Celery", "ferramenta", "fila de tarefas em segundo plano"},
	"click":            {"Click", "ferramenta", "linha de comando com decorators"},
	"typer":            {"Typer", "ferramenta", "linha de comando a partir dos tipos das funções"},
	"rich":             {"Rich", "interface", "texto com cores, tabelas e barras de progresso no terminal"},
	"ruff":             {"Ruff", "ferramenta", "linter e formatador rápido para Python"},
	"mypy":             {"mypy", "ferramenta", "checa as anotações de tipo do Python"},

	// Rust e Ruby
	"tokio":   {"Tokio", "framework", "runtime assíncrono do Rust: tarefas, I/O e temporizadores"},
	"serde":   {"Serde", "biblioteca", "serialização e desserialização (JSON e outros) por derive"},
	"axum":    {"Axum", "http", "framework web sobre o Tokio"},
	"clap":    {"clap", "ferramenta", "linha de comando por derive"},
	"ratatui": {"Ratatui", "interface", "interfaces de terminal em Rust"},
	"rails":   {"Ruby on Rails", "framework", "framework web completo, com convenções no lugar de configuração"},
	"rspec":   {"RSpec", "teste", "testes no estilo descreva/espere"},
}

// known completa nome, tipo e descrição pelo catálogo; fora dele, o nome é o pacote.
func known(t *Tech) {
	pkg := majorSuffix.ReplaceAllString(t.Package, "") // github.com/x/y/v2 → github.com/x/y
	e, ok := catalog[pkg]
	if !ok {
		for k, v := range catalog {
			if strings.HasSuffix(k, "/") && strings.HasPrefix(pkg, k) {
				e, ok = v, true
				break
			}
		}
	}
	if !ok {
		if t.Name == "" {
			t.Name = t.Package
		}
		if t.Kind == "" {
			t.Kind = "biblioteca"
		}
		return
	}
	t.Name, t.Kind, t.What = e.name, e.kind, e.what
}
