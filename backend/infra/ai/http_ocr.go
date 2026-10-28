package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
)

type HTTPOCRConfig struct {
	Endpoint       string
	APIKey         string
	Timeout        time.Duration
	MaxRetries     int
	RetryBaseDelay time.Duration
}

// HTTPOCRAdapter 使用稳定的 multipart/JSON 合约连接任意 OCR 服务，避免业务层绑定供应商 SDK。
type HTTPOCRAdapter struct {
	cfg    HTTPOCRConfig
	client *http.Client
}

type ocrResponse struct {
	Text            string                           `json:"text"`
	Confidence      float64                          `json:"confidence"`
	FieldConfidence map[string]float64               `json:"fieldConfidence"`
	Analysis        *questiondomain.QuestionAnalysis `json:"analysis"`
}

func NewHTTPOCRAdapter(cfg HTTPOCRConfig) *HTTPOCRAdapter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	return &HTTPOCRAdapter{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

func (a *HTTPOCRAdapter) Recognize(ctx context.Context, image io.Reader, imageName string) (domain.QuestionDraft, error) {
	result, err := a.RecognizeWithMetadata(ctx, image, imageName)
	return result.Draft, err
}

func (a *HTTPOCRAdapter) RecognizeWithMetadata(ctx context.Context, image io.Reader, imageName string) (domain.OCRResult, error) {
	if strings.TrimSpace(a.cfg.Endpoint) == "" {
		return domain.OCRResult{}, fmt.Errorf("%w: endpoint is empty", domain.ErrOCRProvider)
	}
	data, err := io.ReadAll(io.LimitReader(image, 10<<20+1))
	if err != nil {
		return domain.OCRResult{}, fmt.Errorf("%w: read image: %v", domain.ErrOCRProvider, err)
	}
	if len(data) > 10<<20 {
		return domain.OCRResult{}, fmt.Errorf("%w: image exceeds 10 MiB", domain.ErrOCRProvider)
	}
	requestID := uuid.NewString()
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		result, retry, err := a.doRequest(ctx, data, imageName, requestID)
		if err == nil {
			return result, nil
		}
		if !retry || attempt == a.cfg.MaxRetries {
			return domain.OCRResult{}, err
		}
		delay := a.cfg.RetryBaseDelay * time.Duration(1<<attempt)
		select {
		case <-ctx.Done():
			return domain.OCRResult{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	return domain.OCRResult{}, fmt.Errorf("%w: retry exhausted", domain.ErrOCRProvider)
}

func (a *HTTPOCRAdapter) doRequest(ctx context.Context, data []byte, imageName, requestID string) (domain.OCRResult, bool, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", imageName)
	if err != nil {
		return domain.OCRResult{}, false, fmt.Errorf("%w: create multipart: %v", domain.ErrOCRProvider, err)
	}
	if _, err = part.Write(data); err != nil {
		return domain.OCRResult{}, false, fmt.Errorf("%w: write image: %v", domain.ErrOCRProvider, err)
	}
	_ = form.WriteField("requestId", requestID)
	if err = form.Close(); err != nil {
		return domain.OCRResult{}, false, fmt.Errorf("%w: close multipart: %v", domain.ErrOCRProvider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Endpoint, &body)
	if err != nil {
		return domain.OCRResult{}, false, fmt.Errorf("%w: create request: %v", domain.ErrOCRProvider, err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Request-ID", requestID)
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return domain.OCRResult{}, true, fmt.Errorf("%w: request: %v", domain.ErrOCRProvider, err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if readErr != nil {
		return domain.OCRResult{}, resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests, fmt.Errorf("%w: read response: %v", domain.ErrOCRProvider, readErr)
	}
	if len(raw) > 1<<20 {
		return domain.OCRResult{}, false, fmt.Errorf("%w: response exceeds 1 MiB", domain.ErrOCRProvider)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.OCRResult{}, true, fmt.Errorf("%w: status %s: %s", domain.ErrOCRProvider, resp.Status, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.OCRResult{}, false, fmt.Errorf("%w: status %s: %s", domain.ErrOCRProvider, resp.Status, strings.TrimSpace(string(raw)))
	}
	var parsed ocrResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return domain.OCRResult{}, false, fmt.Errorf("%w: decode response: %v", domain.ErrOCRInvalidResponse, err)
	}
	if strings.TrimSpace(parsed.Text) == "" || parsed.Confidence < 0 || parsed.Confidence > 1 {
		return domain.OCRResult{}, false, fmt.Errorf("%w: text or confidence is invalid", domain.ErrOCRInvalidResponse)
	}
	for field, confidence := range parsed.FieldConfidence {
		if confidence < 0 || confidence > 1 {
			return domain.OCRResult{}, false, fmt.Errorf("%w: field %s confidence %s is invalid", domain.ErrOCRInvalidResponse, field, strconv.FormatFloat(confidence, 'f', -1, 64))
		}
	}
	return domain.OCRResult{Draft: domain.QuestionDraft{Text: strings.TrimSpace(parsed.Text), Confidence: parsed.Confidence, FieldConfidence: parsed.FieldConfidence}, Analysis: parsed.Analysis, Provider: "http", RequestID: requestID, RawResponse: raw, CreatedAt: domain.Now()}, false, nil
}
