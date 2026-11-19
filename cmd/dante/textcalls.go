package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Alguns modelos (em geral os gratuitos e pequenos) não usam a ferramenta registrar_achado:
// escrevem a chamada como texto, por exemplo
//   registar_achado( arquivo="a.go", linha=10, severidade="alta", titulo="…" )
// e ela apareceria crua no chat. Aqui essas chamadas viram cartões, como se o modelo
// tivesse usado a ferramenta.

var textCallStart = regexp.MustCompile(`(?i)\bregistr?ar_achados?\s*\(`)

// Sinônimos e erros de digitação comuns dos campos.
var findingKeys = map[string]string{
	"arquivo": "arquivo", "file": "arquivo", "path": "arquivo", "caminho": "arquivo",
	"linha": "linha", "line": "linha",
	"severidade": "severidade", "severity": "severidade", "gravidade": "severidade",
	"categoria": "categoria", "category": "categoria",
	"titulo": "titulo", "title": "titulo",
	"explicacao": "explicacao", "explanation": "explicacao", "descricao": "explicacao",
	"sugestao": "sugestao", "sugesto": "sugestao", "suggestion": "sugestao", "correcao": "sugestao",
}

// extractTextFindings tira do texto as chamadas escritas e devolve os achados válidos.
func extractTextFindings(text string) (string, []finding) {
	rest, tagged := extractTagFindings(text)
	rest, called := extractCallFindings(rest)
	return rest, append(tagged, called...)
}

// Achados escritos em tags, como alguns modelos fazem com os tokens do próprio formato de
// ferramentas: registrar_achado <|begin|> <|arquivo|>a.go<|/arquivo|> <|linha|>3<|/linha|> …
var (
	tagOpen   = regexp.MustCompile(`<\|?([A-Za-z_]+)\|?>`)
	tagNoise  = regexp.MustCompile(`<\|[A-Za-z_/]*\|>|\bregistr?ar_achados?\b|\bregister_achado\b`)
	tagMarker = regexp.MustCompile(`<\|?arquivo\|?>`)
)

func extractTagFindings(text string) (string, []finding) {
	locs := tagMarker.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return text, nil
	}
	var out []finding
	var spans [][2]int
	for i, l := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		seg := text[l[0]:end]
		fields := map[string]any{}
		last := 0
		for _, m := range tagOpen.FindAllStringSubmatchIndex(seg, -1) {
			name := seg[m[2]:m[3]]
			if m[0] < last {
				continue
			}
			closeTag := regexp.MustCompile(`<\|?/` + regexp.QuoteMeta(name) + `\|?>`)
			c := closeTag.FindStringIndex(seg[m[1]:])
			if c == nil {
				continue
			}
			fields[name] = strings.TrimSpace(seg[m[1] : m[1]+c[0]])
			last = m[1] + c[1]
		}
		f, err := parseFinding(normalizeFinding(fields))
		if err != nil {
			continue
		}
		out = append(out, f)
		spans = append(spans, [2]int{l[0], l[0] + last})
	}
	if len(out) == 0 {
		return text, nil
	}
	// Tira os trechos dos achados e os tokens soltos ao redor (<|begin|>, registrar_achado…).
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		b.WriteString(text[prev:sp[0]])
		prev = sp[1]
	}
	b.WriteString(text[prev:])
	rest := tagNoise.ReplaceAllString(b.String(), "")
	rest = regexp.MustCompile(`[ \t]+\n`).ReplaceAllString(rest, "\n")
	rest = regexp.MustCompile(`\n{3,}`).ReplaceAllString(rest, "\n\n")
	return strings.TrimSpace(rest), out
}

// extractCallFindings tira do texto as chamadas no formato registrar_achado(chave="valor").
func extractCallFindings(text string) (string, []finding) {
	var out []finding
	var b strings.Builder
	rest := text
	for {
		loc := textCallStart.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(rest)
			break
		}
		args, end, ok := scanCallArgs(rest[loc[1]:])
		if !ok {
			b.WriteString(rest)
			break
		}
		f, err := parseFinding(args)
		if err != nil { // não é um achado válido: deixa o texto como veio
			b.WriteString(rest[:loc[1]+end])
		} else {
			b.WriteString(rest[:loc[0]])
			out = append(out, f)
		}
		rest = rest[loc[1]+end:]
	}
	if len(out) == 0 {
		return text, nil
	}
	// Tira cercas de código e linhas que ficaram vazias onde estavam as chamadas.
	clean := strings.NewReplacer("```python\n```", "", "```\n```", "").Replace(b.String())
	clean = regexp.MustCompile(`\n{3,}`).ReplaceAllString(clean, "\n\n")
	return strings.TrimSpace(clean), out
}

// scanCallArgs lê os argumentos depois do "(" até o ")" que fecha a chamada, fora de
// aspas, e devolve-os como JSON. end é a posição logo depois do ")".
func scanCallArgs(s string) (args string, end int, ok bool) {
	t := strings.TrimLeft(s, " \t\n")
	skip := len(s) - len(t)
	if strings.HasPrefix(t, "{") { // registrar_achado({"arquivo": …})
		dec := json.NewDecoder(strings.NewReader(t))
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return "", 0, false
		}
		n := int(dec.InputOffset())
		after := strings.TrimLeft(t[n:], " \t\n")
		if !strings.HasPrefix(after, ")") {
			return "", 0, false
		}
		end = skip + n + (len(t[n:]) - len(after)) + 1
		return normalizeFinding(m), end, true
	}
	m := map[string]any{}
	i := 0
	for i < len(s) {
		for i < len(s) && strings.ContainsRune(" \t\n,", rune(s[i])) {
			i++
		}
		if i < len(s) && s[i] == ')' {
			return normalizeFinding(m), i + 1, len(m) > 0
		}
		k := i
		for i < len(s) && (s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 0x80) {
			i++
		}
		key := s[k:i]
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if key == "" || i >= len(s) || (s[i] != '=' && s[i] != ':') {
			return "", 0, false
		}
		i++
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return "", 0, false
		}
		var val string
		if q := s[i]; q == '"' || q == '\'' {
			i++
			var v strings.Builder
			for i < len(s) && s[i] != q {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					switch s[i] {
					case 'n':
						v.WriteByte('\n')
					default:
						v.WriteByte(s[i])
					}
				} else {
					v.WriteByte(s[i])
				}
				i++
			}
			if i >= len(s) {
				return "", 0, false
			}
			i++ // aspas de fechamento
			val = v.String()
		} else {
			k := i
			for i < len(s) && s[i] != ',' && s[i] != ')' && s[i] != '\n' {
				i++
			}
			val = strings.TrimSpace(s[k:i])
		}
		m[key] = val
	}
	return "", 0, false
}

// normalizeFinding traduz os campos para os da ferramenta e devolve o JSON.
func normalizeFinding(m map[string]any) string {
	out := map[string]any{}
	for k, v := range m {
		name, ok := findingKeys[fold(k)]
		if !ok {
			continue
		}
		if name == "linha" {
			if s, isStr := v.(string); isStr {
				n, _ := strconv.Atoi(strings.TrimSpace(s))
				v = n
			}
		}
		out[name] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// recoverTextFindings transforma as chamadas escritas como texto na última resposta em
// cartões de achado, logo depois do texto de onde saíram.
func (m *model) recoverTextFindings() {
	start := 0
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].kind == eUser {
			start = i + 1
			break
		}
	}
	var out []entry
	out = append(out, m.entries[:start]...)
	for _, e := range m.entries[start:] {
		if e.kind != eAssistant {
			out = append(out, e)
			continue
		}
		rest, fs := extractTextFindings(e.text)
		if len(fs) == 0 {
			out = append(out, e)
			continue
		}
		if rest != "" {
			out = append(out, entry{kind: eAssistant, text: rest})
		}
		for i := range fs {
			f := fs[i]
			n := 0
			if p, ok := m.resolveRefPath(f.Arquivo); ok {
				f.Arquivo = p
				n = m.addRef(p, f.Linha)
			}
			out = append(out, entry{kind: eFinding, f: &f, ref: n})
		}
	}
	m.entries = out
}
