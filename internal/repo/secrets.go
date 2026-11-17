package repo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Proteção de segredos: arquivos sensíveis não são enviados ao modelo, e valores com
// cara de chave são mascarados em tudo o que as ferramentas devolvem.

// DefaultBlocked são padrões (glob) de arquivos que o modelo não pode ler.
var DefaultBlocked = []string{
	".env", ".env.*", "*.env",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore", "*.kdbx",
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
	"credentials", "credentials.json", "credentials.yml", "credentials.yaml",
	".netrc", ".npmrc", ".pypirc", ".git-credentials", ".htpasswd",
	"*.tfstate", "*.tfstate.*", "secrets.yml", "secrets.yaml", "secrets.json",
}

// DefaultAllowed são exceções: modelos de configuração sem valores reais.
var DefaultAllowed = []string{".env.example", ".env.sample", ".env.template", ".env.dist", "*.pub"}

func matchAny(patterns []string, path string) bool {
	base := filepath.Base(path)
	for _, p := range patterns {
		target := base
		if strings.Contains(p, "/") {
			target = path
		}
		if ok, _ := filepath.Match(p, target); ok {
			return true
		}
	}
	return false
}

// Blocked informa se o arquivo é sensível (padrões do dante + Bloquear, menos Liberar).
func (r Repo) Blocked(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if matchAny(append(append([]string{}, DefaultAllowed...), r.Liberar...), path) {
		return false
	}
	return matchAny(append(append([]string{}, DefaultBlocked...), r.Bloquear...), path)
}

func blockedErr(path string) error {
	return fmt.Errorf("arquivo sensível bloqueado: %s. O dante não envia segredos ao modelo; "+
		"se o arquivo for seguro, libere-o em arquivos_liberados no config.json", path)
}

// ---- mascarar valores ----

const masked = "«segredo omitido»"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`\bsk-(ant-|proj-|or-v1-)?[A-Za-z0-9_\-]{20,}`),                       // Anthropic, OpenAI, OpenRouter, DeepSeek
	regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),                                        // AWS
	regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`),                         // GitHub
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`),                                   // GitHub (fino)
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`),                                      // GitLab
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}\b`),                                  // Slack
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`),                                         // Google
	regexp.MustCompile(`\b(sk|rk)_live_[A-Za-z0-9]{20,}\b`),                                  // Stripe
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), // JWT
}

// chave = "valor" para nomes como password, secret, token, api_key.
var assignPattern = regexp.MustCompile(`(?i)\b([A-Za-z0-9_.\-]*(password|passwd|secret|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)[A-Za-z0-9_.\-]*)(\s*[:=]\s*)(["']?)([^\s"'` + "`" + `,;]{8,})`)

// Redact mascara valores que parecem segredos e diz quantos foram mascarados.
func Redact(s string) (string, int) {
	n := 0
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(string) string { n++; return masked })
	}
	s = assignPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := assignPattern.FindStringSubmatch(m)
		value := parts[5]
		// Referências a variáveis e chamadas não são segredos: os.Getenv("X"), ${X}, process.env.X.
		if strings.ContainsAny(value, "()${}«") || strings.HasPrefix(value, "process.env") {
			return m
		}
		n++
		return parts[1] + parts[3] + parts[4] + masked
	})
	return s, n
}

// filterDiff tira do diff os trechos de arquivos sensíveis.
func (r Repo) filterDiff(diff string) string {
	if !strings.Contains(diff, "diff --git ") {
		return diff
	}
	var out strings.Builder
	sections := strings.Split(diff, "\ndiff --git ")
	for i, sec := range sections {
		if i > 0 {
			sec = "diff --git " + sec
		}
		if path := diffPath(sec); path != "" && r.Blocked(path) {
			fmt.Fprintf(&out, "diff --git a/%s b/%s\n(diff omitido: arquivo sensível)\n", path, path)
			continue
		}
		out.WriteString(sec)
		if i < len(sections)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func diffPath(section string) string {
	first, _, _ := strings.Cut(section, "\n")
	if i := strings.LastIndex(first, " b/"); i >= 0 {
		return first[i+3:]
	}
	return ""
}

// filterGrep tira as linhas de arquivos sensíveis do resultado do git grep.
func (r Repo) filterGrep(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if path, _, ok := strings.Cut(l, ":"); ok && r.Blocked(path) {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}
