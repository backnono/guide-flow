package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	application "github.com/guide-flow/guide-flow/backend/application/research"
	domain "github.com/guide-flow/guide-flow/backend/domain/research"
)

type HTTPConfig struct {
	Endpoint       string
	APIKey         string
	Timeout        time.Duration
	MaxRetries     int
	RetryBaseDelay time.Duration
}

type HTTPAdapter struct {
	cfg    HTTPConfig
	client *http.Client
}

type searchRequest struct {
	Query string `json:"query"`
}

// providerResponse 只描述搜索供应商的边界格式；转换完成后应用层只接收 domain.Context。
type providerResponse struct {
	Query             string                     `json:"query"`
	Status            string                     `json:"status"`
	Sources           []providerSource           `json:"sources"`
	MatchedQuestion   *providerMatchedQuestion   `json:"matchedQuestion"`
	ReferenceSolution *providerReferenceSolution `json:"referenceSolution"`
}

type providerSource struct {
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Summary    string  `json:"summary"`
	TrustScore float64 `json:"trustScore"`
}

type providerMatchedQuestion struct {
	Title                   string   `json:"title"`
	MatchScore              float64  `json:"matchScore"`
	MatchedSubquestionCount int      `json:"matchedSubquestionCount"`
	Checks                  []string `json:"checks"`
}

type providerReferenceSolution struct {
	Status        string   `json:"status"`
	Steps         []string `json:"steps"`
	FinalAnswer   string   `json:"finalAnswer"`
	Uncertainties []string `json:"uncertainties"`
}

func NewHTTPAdapter(cfg HTTPConfig) *HTTPAdapter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	return &HTTPAdapter{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

func (a *HTTPAdapter) Search(ctx context.Context, query string) (domain.Context, error) {
	if strings.TrimSpace(a.cfg.Endpoint) == "" {
		return domain.Context{Query: query, Status: domain.StatusUnavailable, RetrievedAt: time.Now()}, fmt.Errorf("search endpoint is empty")
	}
	payload, err := json.Marshal(searchRequest{Query: query})
	if err != nil {
		return domain.Context{}, err
	}
	var lastErr error
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		result, retry, requestErr := a.doRequest(ctx, payload, query)
		if requestErr == nil {
			return result, nil
		}
		lastErr = requestErr
		if !retry || attempt == a.cfg.MaxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return domain.Context{}, ctx.Err()
		case <-time.After(a.cfg.RetryBaseDelay * time.Duration(1<<attempt)):
		}
	}
	return domain.Context{Query: query, Status: domain.StatusUnavailable, RetrievedAt: time.Now()}, lastErr
}

func (a *HTTPAdapter) doRequest(ctx context.Context, payload []byte, query string) (domain.Context, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return domain.Context{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return domain.Context{}, true, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return domain.Context{}, resp.StatusCode >= 500, err
	}
	if len(raw) > 1<<20 {
		return domain.Context{}, false, fmt.Errorf("search response exceeds 1 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.Context{}, true, fmt.Errorf("search provider status %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.Context{}, false, fmt.Errorf("search provider status %s", resp.Status)
	}
	var response providerResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return domain.Context{}, false, err
	}
	result := domain.Context{Query: response.Query, Status: domain.Status(response.Status), RetrievedAt: time.Now()}
	for _, source := range response.Sources {
		result.Sources = append(result.Sources, domain.Source{Title: source.Title, URL: source.URL, Summary: source.Summary, TrustScore: source.TrustScore})
	}
	if response.MatchedQuestion != nil {
		result.MatchedQuestion = &domain.MatchedQuestion{Title: response.MatchedQuestion.Title, MatchScore: response.MatchedQuestion.MatchScore, MatchedSubquestionCount: response.MatchedQuestion.MatchedSubquestionCount, Checks: append([]string(nil), response.MatchedQuestion.Checks...)}
	}
	if response.ReferenceSolution != nil {
		result.ReferenceSolution = &domain.ReferenceSolution{Status: response.ReferenceSolution.Status, Steps: append([]string(nil), response.ReferenceSolution.Steps...), FinalAnswer: response.ReferenceSolution.FinalAnswer, Uncertainties: append([]string(nil), response.ReferenceSolution.Uncertainties...)}
	}
	if result.Status == "" {
		result.Status = domain.StatusUsed
	}
	if result.Query == "" {
		result.Query = query
	}
	if result.RetrievedAt.IsZero() {
		result.RetrievedAt = time.Now()
	}
	return result, false, nil
}

var _ application.SearchAdapter = (*HTTPAdapter)(nil)
