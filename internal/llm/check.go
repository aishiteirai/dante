package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// ErrInvalidKey indica que o provedor recusou a chave.
var ErrInvalidKey = errors.New("chave recusada pelo provedor")

// CheckKey testa a chave com uma chamada barata (listar modelos), sem gastar tokens.
// Devolve ErrInvalidKey se a chave foi recusada; outros erros significam que não deu
// para verificar (rede, endpoint sem /models...).
func CheckKey(ctx context.Context, p Provider) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if p.Kind == KindAnthropic {
		ro := []option.RequestOption{option.WithAPIKey(p.APIKey), option.WithMaxRetries(0)}
		if p.BaseURL != "" {
			ro = append(ro, option.WithBaseURL(p.BaseURL))
		}
		client := anthropic.NewClient(ro...)
		_, err := client.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1)})
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
			return ErrInvalidKey
		}
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(p.BaseURL, "/")+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("não consegui conectar em %s", p.BaseURL)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return ErrInvalidKey
	case resp.StatusCode >= 300:
		return fmt.Errorf("o provedor respondeu HTTP %d ao teste", resp.StatusCode)
	}
	return nil
}
