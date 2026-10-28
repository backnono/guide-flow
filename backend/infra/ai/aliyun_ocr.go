package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	aliyunocr "github.com/alibabacloud-go/ocr-api-20210707/v3/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type AliyunOCRConfig struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	Timeout         time.Duration
	MaxRetries      int
	RetryBaseDelay  time.Duration
}

// AliyunOCRAdapter 使用教育题目识别接口，直接把上传图片的二进制交给阿里云 SDK。
// 适配器只负责供应商协议转换，题目确认和后续校验仍由应用层统一处理。
type AliyunOCRAdapter struct {
	cfg    AliyunOCRConfig
	client *aliyunocr.Client
}

type aliyunQuestionData struct {
	Content string `json:"content"`
	Words   []struct {
		Probability float64 `json:"prob"`
	} `json:"prism_wordsInfo"`
}

func NewAliyunOCRAdapter(cfg AliyunOCRConfig) *AliyunOCRAdapter {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "ocr-api.cn-hangzhou.aliyuncs.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	config := &openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		Endpoint:        tea.String(cfg.Endpoint),
	}
	client, err := aliyunocr.NewClient(config)
	if err != nil {
		// 构造阶段无法返回错误时保留 nil，首次调用会给出可追踪的配置错误。
		return &AliyunOCRAdapter{cfg: cfg}
	}
	return &AliyunOCRAdapter{cfg: cfg, client: client}
}

func (a *AliyunOCRAdapter) Recognize(ctx context.Context, image io.Reader, imageName string) (domain.QuestionDraft, error) {
	result, err := a.RecognizeWithMetadata(ctx, image, imageName)
	return result.Draft, err
}

func (a *AliyunOCRAdapter) RecognizeWithMetadata(ctx context.Context, image io.Reader, _ string) (domain.OCRResult, error) {
	if a.client == nil {
		return domain.OCRResult{}, fmt.Errorf("%w: initialize aliyun sdk client failed", domain.ErrOCRProvider)
	}
	if strings.TrimSpace(a.cfg.AccessKeyID) == "" || strings.TrimSpace(a.cfg.AccessKeySecret) == "" {
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun access key is empty", domain.ErrOCRProvider)
	}
	data, err := io.ReadAll(io.LimitReader(image, 10<<20+1))
	if err != nil {
		return domain.OCRResult{}, fmt.Errorf("%w: read image: %v", domain.ErrOCRProvider, err)
	}
	if len(data) == 0 || len(data) > 10<<20 {
		return domain.OCRResult{}, fmt.Errorf("%w: image must be between 1 byte and 10 MiB", domain.ErrOCRProvider)
	}

	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		result, err := a.doRecognize(ctx, data)
		if err == nil {
			return result, nil
		}
		if attempt == a.cfg.MaxRetries || ctx.Err() != nil {
			if ctx.Err() != nil {
				return domain.OCRResult{}, ctx.Err()
			}
			return domain.OCRResult{}, err
		}
		select {
		case <-ctx.Done():
			return domain.OCRResult{}, ctx.Err()
		case <-time.After(a.cfg.RetryBaseDelay * time.Duration(1<<attempt)):
		}
	}
	return domain.OCRResult{}, fmt.Errorf("%w: retry exhausted", domain.ErrOCRProvider)
}

func (a *AliyunOCRAdapter) doRecognize(ctx context.Context, data []byte) (domain.OCRResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.OCRResult{}, err
	}
	request := &aliyunocr.RecognizeEduQuestionOcrRequest{
		Body:       bytes.NewReader(data),
		NeedRotate: tea.Bool(true),
	}
	timeoutMillis := int(a.cfg.Timeout / time.Millisecond)
	runtime := (&util.RuntimeOptions{}).
		SetAutoretry(false).
		SetReadTimeout(timeoutMillis).
		SetConnectTimeout(timeoutMillis)
	response, err := a.client.RecognizeEduQuestionOcrWithOptions(request, runtime)
	if err != nil {
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun request: %v", domain.ErrOCRProvider, err)
	}
	if response == nil || response.Body == nil {
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun response is empty", domain.ErrOCRInvalidResponse)
	}
	if response.Body.Code != nil && strings.TrimSpace(*response.Body.Code) != "" {
		message := ""
		if response.Body.Message != nil {
			message = *response.Body.Message
		}
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun code %s: %s", domain.ErrOCRProvider, *response.Body.Code, message)
	}
	if response.Body.Data == nil || strings.TrimSpace(*response.Body.Data) == "" {
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun data is empty", domain.ErrOCRInvalidResponse)
	}

	var parsed aliyunQuestionData
	if err := json.Unmarshal([]byte(*response.Body.Data), &parsed); err != nil {
		return domain.OCRResult{}, fmt.Errorf("%w: decode aliyun data: %v", domain.ErrOCRInvalidResponse, err)
	}
	text := strings.TrimSpace(parsed.Content)
	if text == "" {
		return domain.OCRResult{}, fmt.Errorf("%w: aliyun returned empty content", domain.ErrOCRUnrecognized)
	}
	rawResponse, _ := json.Marshal(response.Body)
	confidence := 0.75
	for _, word := range parsed.Words {
		if word.Probability <= 0 {
			continue
		}
		probability := word.Probability / 100
		if confidence == 0.75 || probability < confidence {
			confidence = probability
		}
	}
	requestID := ""
	if response.Body.RequestId != nil {
		requestID = *response.Body.RequestId
	}
	return domain.OCRResult{
		Draft: domain.QuestionDraft{
			Text:            text,
			Confidence:      confidence,
			FieldConfidence: map[string]float64{"text": confidence},
		},
		Provider:    "aliyun",
		RequestID:   requestID,
		RawResponse: rawResponse,
		CreatedAt:   domain.Now(),
	}, nil
}
