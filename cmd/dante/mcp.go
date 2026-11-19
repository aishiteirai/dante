package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/mapa"
	"github.com/aishiteirai/dante/internal/repo"
)

// Servidor MCP (dante mcp): o Claude Code e outros clientes MCP usam o dante como fonte de
// contexto. As ferramentas são fixas (git, grep, linters, mapa) e não chamam modelo nenhum:
// o próprio agente é o modelo, e o dante só lhe entrega o contexto certo, barato e enxuto.
// Protocolo: JSON-RPC 2.0, uma mensagem por linha no stdin/stdout.

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	run         func(m model, args map[string]any) (string, error)
}

func mcpTools() []mcpTool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	obj := func(props map[string]any, req ...string) map[string]any {
		if req == nil {
			req = []string{}
		}
		return map[string]any{"type": "object", "properties": props, "required": req}
	}
	alvo := str("Opcional: \"local\" (mudanças locais), um commit, ou base...branch. Sem ele: as mudanças locais; sem elas, a branch atual × main.")
	return []mcpTool{
		{Name: "dante_onde", Description: "Acha onde mexer no repositório para uma tarefa: busca os termos no código (sem diferenciar maiúsculas), " +
			"ordena os arquivos por relevância (termos raros valem mais; testes e docs menos) e devolve as linhas que bateram e o esqueleto dos principais. " +
			"Passe nomes prováveis de funções, tipos, rotas ou textos, em inglês e no idioma do projeto.",
			InputSchema: obj(map[string]any{"termos": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "De 4 a 12 termos de busca."}}, "termos"), run: mcpOnde},
		{Name: "dante_funcao", Description: "Contexto de uma função: o corpo (com o comentário que a documenta), onde ela é usada no projeto " +
			"(com a linha de código de cada uso) e as funções do projeto que ela chama. Use antes de mudar uma função, para ver o impacto.",
			InputSchema: obj(map[string]any{"arquivo": str("Arquivo (com linha) ou só o arquivo."), "linha": map[string]any{"type": "integer"},
				"nome": str("Ou: o nome da função.")}), run: mcpFuncao},
		{Name: "dante_revisao", Description: "Pacote de revisão das mudanças: o diff do código (3 linhas de contexto), os arquivos que ficaram fora " +
			"(testes, docs, gerados), onde as funções alteradas são usadas, as listas de verificação que valem para o diff e os problemas novos " +
			"das ferramentas do projeto (se permitidas). Use para revisar o que você mesmo mudou antes de terminar.",
			InputSchema: obj(map[string]any{"alvo": alvo}), run: mcpRevisao},
		{Name: "dante_verificar", Description: "Roda as ferramentas de verificação do projeto (Qlty, Biome, Oxlint, ESLint, tsc, ruff, go vet, " +
			"golangci-lint, clippy) nos arquivos alterados e devolve só os problemas que a mudança introduziu. Precisa da permissão do repositório " +
			"(dante hook instalar, ou /verificar na interface).",
			InputSchema: obj(map[string]any{"alvo": alvo}), run: mcpVerificar},
		{Name: "dante_mapa", Description: "O mapa do projeto gerado pelo dante (/mapa): visão geral, por onde começar e uma linha por pasta. " +
			"Sem mapa, devolve as pastas do projeto com a quantidade de arquivos.",
			InputSchema: obj(map[string]any{}), run: mcpMapa},
		{Name: "dante_quem_usa", Description: "Quem usa um arquivo: os outros arquivos que o citam (em Go, quem importa a pasta dele).",
			InputSchema: obj(map[string]any{"arquivo": str("Caminho a partir da raiz.")}, "arquivo"), run: mcpQuemUsa},
	}
}

// runMCP atende o cliente MCP pelo stdin/stdout até o fim da entrada.
func runMCP(cfg config.Config, in io.Reader, out io.Writer) int {
	tools := mcpTools()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if len(req.ID) == 0 { // notificação (ex.: notifications/initialized): sem resposta
			continue
		}
		reply := func(result any, err *rpcError) {
			msg := map[string]any{"jsonrpc": "2.0", "id": req.ID}
			if err != nil {
				msg["error"] = err
			} else {
				msg["result"] = result
			}
			_ = enc.Encode(msg)
		}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			reply(map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "dante", "version": version},
				"instructions": "O dante entrega contexto do repositório sem gastar tokens: use dante_onde antes de procurar onde mexer, " +
					"dante_funcao antes de mudar uma função e dante_revisao para revisar o que você mudou antes de terminar."}, nil)
		case "ping":
			reply(map[string]any{}, nil)
		case "tools/list":
			reply(map[string]any{"tools": tools}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text, isErr := callMCPTool(cfg, tools, p.Name, p.Arguments)
			reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isErr}, nil)
		default:
			reply(nil, &rpcError{Code: -32601, Message: "método desconhecido: " + req.Method})
		}
	}
	return 0
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func callMCPTool(cfg config.Config, tools []mcpTool, name string, args map[string]any) (string, bool) {
	for _, t := range tools {
		if t.Name != name {
			continue
		}
		m, err := headlessModel(cfg, "", argStr(args, "alvo"), "review")
		if err != nil {
			return err.Error(), true
		}
		text, err := t.run(m, args)
		if err != nil {
			return err.Error(), true
		}
		red, _ := repo.Redact(text)
		return red, false
	}
	return "ferramenta desconhecida: " + name, true
}

func argStr(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}

func mcpOnde(m model, args map[string]any) (string, error) {
	var terms []string
	if xs, ok := args["termos"].([]any); ok {
		for _, x := range xs {
			if s, ok := x.(string); ok {
				terms = append(terms, s)
			}
		}
	}
	if len(terms) == 0 {
		return "", fmt.Errorf("passe os termos de busca em \"termos\"")
	}
	hits := m.repo.Locate(terms, 8)
	if len(hits) == 0 {
		return "Nenhum trecho encontrado para: " + strings.Join(terms, ", "), nil
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "## %s\n", h.Path)
		for _, l := range h.Lines {
			fmt.Fprintf(&b, "%d: %s\n", l.Line, l.Text)
		}
		if i < 3 {
			if ls, err := m.repo.FileContent(repo.WorkTree, h.Path); err == nil {
				if o := repo.FileOutline(h.Path, ls); o != "" {
					b.WriteString("esqueleto:\n" + firstLines(o, 25) + "\n")
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func mcpFuncao(m model, args map[string]any) (string, error) {
	file, name := argStr(args, "arquivo"), argStr(args, "nome")
	line := 0
	if f, ok := args["linha"].(float64); ok {
		line = int(f)
	}
	if p, l, ok := strings.Cut(file, ":"); ok {
		file = p
		fmt.Sscan(l, &line)
	}
	if file == "" && name != "" {
		p, l, ok := m.repo.FindFunc(repo.WorkTree, name)
		if !ok {
			return "", fmt.Errorf("não achei a função %s", name)
		}
		file, line = p, l
	}
	if file == "" {
		return "", fmt.Errorf("passe \"arquivo\" e \"linha\", ou \"nome\"")
	}
	fi, err := m.repo.FuncAt(repo.WorkTree, file, line)
	if err != nil {
		return "", err
	}
	// O pacote da explicação, sem a pergunta do fim (quem pergunta é o agente).
	p := m.funcPrompt(funcPackMsg{info: fi, version: "o disco"})
	if i := strings.LastIndex(p, "\n"); i >= 0 {
		p = p[:i]
	}
	return strings.TrimSpace(p), nil
}

func mcpRevisao(m model, _ map[string]any) (string, error) {
	if len(m.files) == 0 {
		return "Não há mudanças em " + targetName(m.target) + ".", nil
	}
	m.task = taskRevisar
	var issues []checks.Issue
	allowed := m.cfg.ChecksAllowed(m.repo.Root)
	if allowed {
		issues = headlessChecks(m, true)
	}
	b := m.contextBlock() + m.diffBlock("")
	if allowed {
		b += checksBlock(issues)
	}
	return b, nil
}

func mcpVerificar(m model, _ map[string]any) (string, error) {
	if !m.cfg.ChecksAllowed(m.repo.Root) {
		return "", fmt.Errorf("as verificações não estão permitidas neste repositório: rode \"dante hook instalar\" ou use /verificar na interface")
	}
	is := headlessChecks(m, true)
	if len(is) == 0 {
		return "Nenhum problema novo nas verificações do projeto (" + targetName(m.target) + ").", nil
	}
	return issuesMarkdown(is), nil
}

func mcpMapa(m model, _ map[string]any) (string, error) {
	if mp, err := mapa.Load(mapPath(m.repo.Root)); err == nil {
		return "Mapa gerado no commit " + shortSHA(mp.Commit) + " (pode estar desatualizado):\n\n" + mp.Compact(8000), nil
	}
	files, err := m.repo.ListFiles()
	if err != nil {
		return "", err
	}
	return "Ainda não há mapa (gere com /mapa na interface). Pastas do projeto:\n" + folderList(files, 80), nil
}

func mcpQuemUsa(m model, args map[string]any) (string, error) {
	p := argStr(args, "arquivo")
	if p == "" {
		return "", fmt.Errorf("passe \"arquivo\"")
	}
	us := m.repo.FileUsers(repo.WorkTree, p, 20)
	if len(us) == 0 {
		return "Nenhum outro arquivo cita " + p + ".", nil
	}
	var b strings.Builder
	for _, u := range us {
		fmt.Fprintf(&b, "- %s:%d  %s\n", u.Path, u.Line, u.Text)
	}
	return b.String(), nil
}

// installMCP registra o servidor no .mcp.json do projeto (escopo do projeto no Claude Code).
func installMCP() int {
	wd, _ := os.Getwd()
	root, err := repo.Root(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	path := filepath.Join(root, ".mcp.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "dante: %s não é um JSON válido; não mexi nele\n", path)
			return 2
		}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if _, ok := servers["dante"]; ok {
		fmt.Println("O servidor MCP do dante já está em", path)
		return 0
	}
	servers["dante"] = map[string]any{"command": danteBin(), "args": []string{"mcp"}}
	cfg["mcpServers"] = servers
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	fmt.Printf("Servidor MCP do dante registrado em %s.\n", path)
	fmt.Println("Na próxima vez que abrir o Claude Code neste projeto, aprove o servidor \"dante\". As ferramentas")
	fmt.Println("dante_onde, dante_funcao, dante_revisao, dante_verificar, dante_mapa e dante_quem_usa não gastam tokens do dante.")
	return 0
}
