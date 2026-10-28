package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	apihttp "github.com/guide-flow/guide-flow/backend/api/http"
	"github.com/guide-flow/guide-flow/backend/application/lesson"
	"github.com/guide-flow/guide-flow/backend/application/research"
	"github.com/guide-flow/guide-flow/backend/application/retrieval"
	"github.com/guide-flow/guide-flow/backend/application/solving"
	"github.com/guide-flow/guide-flow/backend/infra/ai"
	"github.com/guide-flow/guide-flow/backend/infra/config"
	"github.com/guide-flow/guide-flow/backend/infra/objectstore"
	memoryinfra "github.com/guide-flow/guide-flow/backend/infra/persistence/memory"
	mysqlinfra "github.com/guide-flow/guide-flow/backend/infra/persistence/mysql"
	"github.com/guide-flow/guide-flow/backend/infra/queue"
	researchinfra "github.com/guide-flow/guide-flow/backend/infra/research"
	"golang.org/x/term"
)

type App struct {
	Config  config.Config
	Handler http.Handler
	Close   func() error
}

func Build(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := prepareLLMConfig(&cfg); err != nil {
		return nil, err
	}
	repo, closeRepo, err := buildLessonRepository(ctx, cfg)
	if err != nil {
		return nil, err
	}
	images, err := buildImageStore(cfg)
	if err != nil {
		_ = closeRepo()
		return nil, err
	}
	jobQueuePort, runner, closeQueue, err := buildJobQueue(ctx, cfg)
	if err != nil {
		_ = closeRepo()
		return nil, err
	}
	service := lesson.NewService(repo, jobQueuePort, images)
	appCtx, cancel := context.WithCancel(ctx)
	var attempts lesson.JobAttemptRecorder
	if recorder, ok := repo.(lesson.JobAttemptRecorder); ok {
		attempts = recorder
	}
	ocr := buildOCR(cfg)
	matcher := lesson.SolutionMatcher(retrieval.NoopMatcher{})
	var templateService retrieval.TemplateService
	if templateStore, ok := repo.(retrieval.TemplateStore); ok {
		templateService = retrieval.NewService(templateStore)
		matcher = retrieval.NewStructuredMatcher(templateStore)
	}
	if candidateMatcher, ok := repo.(lesson.SolutionMatcher); ok {
		matcher = candidateMatcher
	}
	// 通过应用层契约装配检索器；MySQL 使用结构化匹配，其他环境使用 Noop Adapter。
	worker := lesson.NewWorkerWithDependencies(
		repo,
		ocr,
		ai.RuleBasedStructurer{},
		matcher,
		buildSolver(cfg),
		ai.AdaptivePlanner{},
		ai.PassRenderer{},
		images,
		attempts,
	)
	if vision := buildVisionAnalyzer(cfg); vision != nil {
		worker.WithVisionAnalyzer(vision)
	}
	worker.WithResearchAdapters(researchRouter(), buildSearch(cfg))
	go runner.Run(
		appCtx, func(processCtx context.Context, job queue.Job) error {
			return worker.ProcessJob(
				processCtx,
				lesson.JobMetadata{ID: job.ID, LessonID: job.LessonID, Attempt: job.Attempt},
			)
		},
	)
	if outbox, ok := repo.(lesson.OutboxRepository); ok {
		go runOutboxPublisher(appCtx, outbox, jobQueuePort)
	}
	return &App{
		Config: cfg, Handler: apihttp.NewHandler(service, ocr, templateService), Close: func() error {
			cancel()
			queueErr := closeQueue()
			repoErr := closeRepo()
			if queueErr != nil {
				return queueErr
			}
			return repoErr
		},
	}, nil
}

func prepareLLMConfig(cfg *config.Config) error {
	if !cfg.LLM.RequiresAPIKey() || cfg.LLM.APIKey != "" {
		return cfg.LLM.Validate()
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return cfg.LLM.Validate()
	}

	fmt.Fprint(os.Stderr, "请输入 DeepSeek API Key（输入内容不会回显）：")
	key, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read llm api key: %w; or set APP_LLM_API_KEY/DEEPSEEK_API_KEY before starting the server", err)
	}
	cfg.LLM.APIKey = string(key)
	return cfg.LLM.Validate()
}

func buildSolver(cfg config.Config) lesson.Solver {
	if cfg.LLM.Provider == "http" || cfg.LLM.Provider == "deepseek" {
		generator := ai.NewHTTPLLMSolver(
			ai.HTTPLLMConfig{
				Provider:          cfg.LLM.Provider,
				Endpoint:          cfg.LLM.Endpoint,
				APIKey:            cfg.LLM.APIKey,
				Model:             cfg.LLM.Model,
				Timeout:           time.Duration(cfg.LLM.TimeoutSecs) * time.Second,
				MaxRetries:        cfg.LLM.MaxRetries,
				RetryBaseDelay:    time.Duration(cfg.LLM.RetryBaseDelayMillis) * time.Millisecond,
				StreamIdleTimeout: time.Duration(cfg.LLM.StreamIdleTimeoutSecs) * time.Second,
			},
		)
		return solving.NewLLMSolverWithVerifier(generator, solving.NewVerifier(solving.NewVerifierRegistry()))
	}
	return ai.DemoSolver{}
}

func buildVisionAnalyzer(cfg config.Config) lesson.StructuredQuestionAnalyzer {
	if cfg.LLM.Provider != "deepseek" {
		return nil
	}
	return ai.NewDeepSeekVisionAnalyzer(ai.HTTPLLMConfig{
		Provider: cfg.LLM.Provider, Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model,
		Timeout:    time.Duration(cfg.LLM.TimeoutSecs) * time.Second,
		MaxRetries: cfg.LLM.MaxRetries, RetryBaseDelay: time.Duration(cfg.LLM.RetryBaseDelayMillis) * time.Millisecond,
		MaxTokens: cfg.LLM.MaxTokens,
	})
}

func researchRouter() research.Router { return research.Router{} }

func buildSearch(cfg config.Config) research.SearchAdapter {
	if cfg.Search.Provider == "deepseek" {
		searchAPIKey := cfg.Search.APIKey
		if searchAPIKey == "" {
			searchAPIKey = cfg.LLM.APIKey
		}
		return researchinfra.NewDeepSeekWebSearchAdapter(researchinfra.DeepSeekWebSearchConfig{
			DeepSeekEndpoint: cfg.LLM.Endpoint,
			DeepSeekAPIKey:   cfg.LLM.APIKey,
			DeepSeekModel:    cfg.LLM.Model,
			SearchEndpoint:   cfg.Search.Endpoint,
			SearchAPIKey:     searchAPIKey,
			Timeout:          time.Duration(cfg.Search.TimeoutSecs) * time.Second,
			MaxRetries:       cfg.Search.MaxRetries,
			RetryBaseDelay:   time.Duration(cfg.Search.RetryBaseDelayMillis) * time.Millisecond,
			MaxToolCalls:     cfg.Search.MaxToolCalls,
		})
	}
	if cfg.Search.Provider == "http" {
		return researchinfra.NewHTTPAdapter(
			researchinfra.HTTPConfig{
				Endpoint: cfg.Search.Endpoint, APIKey: cfg.Search.APIKey,
				Timeout:        time.Duration(cfg.Search.TimeoutSecs) * time.Second,
				MaxRetries:     cfg.Search.MaxRetries,
				RetryBaseDelay: time.Duration(cfg.Search.RetryBaseDelayMillis) * time.Millisecond,
			},
		)
	}
	return research.NoopSearchAdapter{}
}

func runOutboxPublisher(ctx context.Context, outbox lesson.OutboxRepository, queue lesson.JobQueue) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	publish := func() {
		events, err := outbox.PendingOutbox(ctx, 20)
		if err != nil {
			return
		}
		for _, event := range events {
			if err := queue.Enqueue(event.LessonID); err != nil {
				continue
			}
			// 先成功投递再标记 published；进程在两者之间退出时允许重复投递，队列消费端必须幂等。
			_ = outbox.MarkOutboxPublished(ctx, event.ID)
		}
	}
	publish()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}

func buildOCR(cfg config.Config) lesson.OcrAdapter {
	switch cfg.OCR.Provider {
	case "aliyun":
		return ai.NewAliyunOCRAdapter(ai.AliyunOCRConfig{
			Endpoint:        cfg.OCR.Endpoint,
			AccessKeyID:     cfg.OCR.AccessKeyID,
			AccessKeySecret: cfg.OCR.AccessKeySecret,
			Timeout:         time.Duration(cfg.OCR.TimeoutSecs) * time.Second,
			MaxRetries:      cfg.OCR.MaxRetries,
			RetryBaseDelay:  time.Duration(cfg.OCR.RetryBaseDelayMillis) * time.Millisecond,
		})
	case "http":
		return ai.NewHTTPOCRAdapter(
			ai.HTTPOCRConfig{
				Endpoint: cfg.OCR.Endpoint, APIKey: cfg.OCR.APIKey,
				Timeout:        time.Duration(cfg.OCR.TimeoutSecs) * time.Second,
				MaxRetries:     cfg.OCR.MaxRetries,
				RetryBaseDelay: time.Duration(cfg.OCR.RetryBaseDelayMillis) * time.Millisecond,
			},
		)
	default:
		return ai.DemoOCR{}
	}
}

type queueRunner interface {
	Run(context.Context, queue.Processor)
}

func buildJobQueue(ctx context.Context, cfg config.Config) (lesson.JobQueue, queueRunner, func() error, error) {
	if cfg.Redis.Addr == "" {
		memoryQueue := queue.NewQueue(32)
		return memoryQueue, memoryQueue, func() error { return nil }, nil
	}
	redisQueue, err := queue.NewRedisQueue(
		ctx, queue.RedisConfig{
			Addr:              cfg.Redis.Addr,
			Password:          cfg.Redis.Password,
			DB:                cfg.Redis.DB,
			Stream:            cfg.Redis.Stream,
			Group:             cfg.Redis.Group,
			Consumer:          cfg.Redis.Consumer,
			VisibilityTimeout: time.Duration(cfg.Redis.VisibilityTimeoutSecs) * time.Second,
			MaxAttempts:       cfg.Redis.MaxAttempts,
			RetryBaseDelay:    time.Duration(cfg.Redis.RetryBaseDelaySecs) * time.Second,
			JobTimeout:        time.Duration(cfg.Redis.JobTimeoutSecs) * time.Second,
			ClaimInterval:     time.Duration(cfg.Redis.ClaimIntervalSecs) * time.Second,
			ReadBlock:         time.Duration(cfg.Redis.ReadBlockMillis) * time.Millisecond,
		},
	)
	if err != nil {
		return nil, nil, func() error { return nil }, err
	}
	return redisQueue, redisQueue, redisQueue.Close, nil
}

func buildImageStore(cfg config.Config) (lesson.ImageStore, error) {
	store := cfg.ObjectStore
	if store.Endpoint == "" {
		// 未配置对象存储时保留内存实现，方便本地 Demo；生产配置 endpoint 后自动切换到 S3。
		return objectstore.NewMemoryImageStore(), nil
	}
	imageStore, err := objectstore.NewS3ImageStore(
		objectstore.S3ImageStoreConfig{
			Endpoint:        store.Endpoint,
			Region:          store.Region,
			Bucket:          store.Bucket,
			AccessKey:       store.AccessKey,
			SecretKey:       store.SecretKey,
			UsePathStyle:    store.UsePathStyle,
			KeyPrefix:       store.KeyPrefix,
			PresignValidity: time.Duration(store.PresignMinutes) * time.Minute,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build image store: %w", err)
	}
	return imageStore, nil
}

func buildLessonRepository(ctx context.Context, cfg config.Config) (lesson.LessonRepository, func() error, error) {
	if cfg.Database.MySQLDSN == "" {
		// 未配置数据库时保留内存实现，保证 Demo 和单元测试不依赖外部服务。
		return memoryinfra.NewLessonRepository(), func() error { return nil }, nil
	}
	db, err := mysqlinfra.Open(cfg.Database.MySQLDSN)
	if err != nil {
		return nil, nil, err
	}
	if err = mysqlinfra.AutoMigrate(db); err != nil {
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
		return nil, nil, fmt.Errorf("auto migrate mysql: %w", err)
	}
	return mysqlinfra.NewLessonRepository(db), func() error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}, nil
}
