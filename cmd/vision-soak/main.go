// vision-soak 只负责真实图片循环和产物留档，解析规则仍由 backend/infra/ai 提供。
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guide-flow/guide-flow/backend/infra/ai"
	"github.com/guide-flow/guide-flow/backend/infra/config"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) != "" {
		*s = append(*s, value)
	}
	return nil
}

type runRecord struct {
	RunID          string   `json:"runId"`
	CaseID         string   `json:"caseId"`
	Round          int      `json:"round"`
	ImagePath      string   `json:"imagePath"`
	ImageSHA256    string   `json:"imageSha256"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	PromptVersion  string   `json:"promptVersion"`
	SchemaVersion  string   `json:"schemaVersion"`
	StartedAt      string   `json:"startedAt"`
	FinishedAt     string   `json:"finishedAt"`
	DurationMs     int64    `json:"durationMs"`
	Requests       int      `json:"requests"`
	FinishReason   string   `json:"finishReason,omitempty"`
	ParseError     string   `json:"parseError,omitempty"`
	ParseErrorPath string   `json:"parseErrorPath,omitempty"`
	Asks           []string `json:"asks,omitempty"`
	Figures        int      `json:"figures,omitempty"`
	Error          string   `json:"error,omitempty"`
	RawFile        string   `json:"rawFile,omitempty"`
}

func main() {
	var images stringList
	flag.Var(&images, "image", "图片路径，可重复指定")
	rounds := flag.Int("rounds", 5, "每张图片的基础请求轮数")
	delay := flag.Duration("delay", 2*time.Second, "请求间隔")
	parallel := flag.Int("parallel", 1, "并发请求数")
	maxRequests := flag.Int("max-requests", 0, "包含纠错请求在内的外部请求上限；0 表示不额外限制")
	stopAfterClean := flag.Int("stop-after-clean", 0, "连续无新失败指纹后停止；0 表示不启用")
	maxUniqueFailures := flag.Int("max-unique-failures", 20, "唯一失败指纹上限")
	output := flag.String("output", "", "产物目录；默认 var/vision-soak/<run-id>")
	continuous := flag.Bool("continuous", false, "允许在 rounds 完成后持续运行")
	flag.Parse()

	if len(images) == 0 {
		images = append(
			images,
			// "/Users/nono/Downloads/几何 2.png", "/Users/nono/Downloads/几何.png",
			"/Users/nono/Downloads/几何3.png", "/Users/nono/Downloads/几何4.png",
			"/Users/nono/Downloads/几何难1.png", "/Users/nono/Downloads/几何难2.png",
		)
	}
	if *rounds < 1 || *parallel < 1 || *maxUniqueFailures < 1 {
		fatalf("rounds、parallel 和 max-unique-failures 必须为正数")
	}
	cfg, err := config.Load()
	if err != nil {
		fatalf("读取配置失败: %v", err)
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		fatalf("未找到 API Key，请通过 APP_LLM_API_KEY 或 DEEPSEEK_API_KEY 注入")
	}
	if strings.TrimSpace(cfg.LLM.Endpoint) == "" || strings.TrimSpace(cfg.LLM.Model) == "" {
		fatalf("LLM endpoint/model 未配置")
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	root := *output
	if root == "" {
		root = filepath.Join("var", "vision-soak", runID)
	}
	for _, name := range []string{"raw", "normalized", "failures"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			fatalf("创建产物目录失败: %v", err)
		}
	}
	writeJSON(
		filepath.Join(root, "manifest.json"), map[string]any{
			"runId": runID, "images": images, "rounds": *rounds, "delay": delay.String(),
			"parallel": *parallel, "maxRequests": *maxRequests, "continuous": *continuous,
			"promptVersion": ai.VisionPromptVersion, "schemaVersion": ai.VisionSchemaVersion,
		},
	)

	analyzer := ai.NewDeepSeekVisionAnalyzer(
		ai.HTTPLLMConfig{
			Provider:       cfg.LLM.Provider,
			Endpoint:       cfg.LLM.Endpoint,
			APIKey:         cfg.LLM.APIKey,
			Model:          cfg.LLM.Model,
			Timeout:        time.Duration(cfg.LLM.TimeoutSecs) * time.Second,
			MaxRetries:     cfg.LLM.MaxRetries,
			RetryBaseDelay: time.Duration(cfg.LLM.RetryBaseDelayMillis) * time.Millisecond,
			MaxTokens:      cfg.LLM.MaxTokens,
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var recordsMu sync.Mutex
	var records []runRecord
	seenFailures := map[string]int{}
	requestCount := 0
	cleanRounds := 0
	ndjson, err := os.Create(filepath.Join(root, "runs.ndjson"))
	if err != nil {
		fatalf("创建 runs.ndjson 失败: %v", err)
	}
	defer ndjson.Close()

	for round := 1; ; round++ {
		beforeFailures := len(seenFailures)
		sem := make(chan struct{}, *parallel)
		var wg sync.WaitGroup
		for index, imagePath := range images {
			if *maxRequests > 0 && requestCount >= *maxRequests {
				break
			}
			wg.Add(1)
			go func(index int, imagePath string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				record := analyzeOne(ctx, analyzer, runID, cfg.LLM.Model, index+1, round, imagePath, root)
				recordsMu.Lock()
				requestCount += record.Requests // 使用适配器统计值，确保一次纠错也计入硬上限。
				records = append(records, record)
				if record.Error != "" || record.ParseError != "" {
					fingerprint := failureFingerprint(record)
					seenFailures[fingerprint]++
				}
				encoded, _ := json.Marshal(record)
				_, _ = ndjson.Write(append(encoded, '\n'))
				recordsMu.Unlock()
			}(index, imagePath)
			if *delay > 0 {
				time.Sleep(*delay)
			}
		}
		wg.Wait()
		if len(seenFailures) == beforeFailures {
			cleanRounds++
		} else {
			cleanRounds = 0
		}
		if len(seenFailures) >= *maxUniqueFailures || (*stopAfterClean > 0 && cleanRounds >= *stopAfterClean) {
			break
		}
		if !*continuous && round >= *rounds {
			break
		}
	}

	sort.Slice(records, func(i, j int) bool { return records[i].Round < records[j].Round })
	summary := map[string]any{
		"runId":          runID,
		"total":          len(records),
		"uniqueFailures": len(seenFailures),
		"records":        records,
	}
	writeJSON(filepath.Join(root, "summary.json"), summary)
	writeReport(filepath.Join(root, "report.md"), runID, records, seenFailures)
	fmt.Printf("vision soak 完成: %s\n", root)
}

// failureFingerprint 将供应商状态错误归并为稳定指纹，避免 request_id 等瞬时信息制造大量假象。
func failureFingerprint(record runRecord) string {
	path := record.ParseErrorPath
	message := record.ParseError
	if message == "" {
		path = "request"
		switch {
		case strings.Contains(record.Error, "status 402 Payment Required"):
			message = "provider status 402 Payment Required"
		case strings.Contains(record.Error, "deepseek vision status"):
			message = "provider request failed"
		default:
			message = "request failed"
		}
	}
	return record.PromptVersion + "|" + path + "|" + message
}

func analyzeOne(
	ctx context.Context,
	analyzer *ai.DeepSeekVisionAnalyzer,
	runID, model string,
	caseNo, round int,
	imagePath, root string,
) runRecord {
	started := time.Now().UTC()
	record := runRecord{
		RunID:         runID,
		CaseID:        fmt.Sprintf("image-%d", caseNo),
		Round:         round,
		ImagePath:     imagePath,
		Provider:      "deepseek-vision",
		Model:         model,
		PromptVersion: ai.VisionPromptVersion,
		SchemaVersion: ai.VisionSchemaVersion,
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	data, err := os.ReadFile(imagePath)
	if err != nil {
		record.Error = err.Error()
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return record
	}
	hash := sha256.Sum256(data)
	record.ImageSHA256 = hex.EncodeToString(hash[:])
	result, err := analyzer.Analyze(ctx, bytes.NewReader(data), filepath.Base(imagePath))
	if err != nil {
		record.Error = err.Error()
	}
	record.Provider, record.PromptVersion, record.SchemaVersion = result.Provider, result.PromptVersion, result.SchemaVersion
	record.FinishReason, record.ParseError, record.ParseErrorPath, record.DurationMs = result.FinishReason, result.ParseError, result.ParseErrorPath, result.DurationMs
	record.Requests = result.RequestCount
	if result.StructuredQuestion != nil {
		record.Asks = result.StructuredQuestion.Asks
		if result.StructuredQuestion.Diagram != nil {
			record.Figures = len(result.StructuredQuestion.Diagram.Figures)
		}
	}
	if len(result.RawResponse) > 0 {
		rawName := fmt.Sprintf("%s-round-%02d.json", record.CaseID, round)
		record.RawFile = filepath.Join("raw", rawName)
		_ = os.WriteFile(filepath.Join(root, record.RawFile), result.RawResponse, 0o600)
	}
	record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return record
}

func writeJSON(path string, value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	_ = os.WriteFile(path, data, 0o600)
}

func writeReport(path, runID string, records []runRecord, failures map[string]int) {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"# Vision soak 报告\n\n- run ID: `%s`\n- 总轮次记录: %d\n- 成功: %d\n- 唯一失败指纹: %d\n\n## 失败指纹\n\n",
		runID,
		len(records),
		countSuccess(records),
		len(failures),
	)
	keys := make([]string, 0, len(failures))
	for key := range failures {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&builder, "- `%s`: %d\n", key, failures[key])
	}
	_ = os.WriteFile(path, []byte(builder.String()), 0o600)
}

func countSuccess(records []runRecord) int {
	count := 0
	for _, record := range records {
		if record.Error == "" && record.ParseError == "" {
			count++
		}
	}
	return count
}
func fatalf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
