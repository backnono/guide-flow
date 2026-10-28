package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Database    DatabaseConfig    `mapstructure:"database"`
	ObjectStore ObjectStoreConfig `mapstructure:"object_store"`
	Redis       RedisConfig       `mapstructure:"redis"`
	OCR         OCRConfig         `mapstructure:"ocr"`
	Search      SearchConfig      `mapstructure:"search"`
	LLM         LLMConfig         `mapstructure:"llm"`
}

type ServerConfig struct {
	HTTPAddr string `mapstructure:"http_addr"`
}

type DatabaseConfig struct {
	MySQLDSN string `mapstructure:"mysql_dsn"`
}

type ObjectStoreConfig struct {
	Endpoint       string `mapstructure:"endpoint"`
	Region         string `mapstructure:"region"`
	Bucket         string `mapstructure:"bucket"`
	AccessKey      string `mapstructure:"access_key"`
	SecretKey      string `mapstructure:"secret_key"`
	UsePathStyle   bool   `mapstructure:"use_path_style"`
	KeyPrefix      string `mapstructure:"key_prefix"`
	PresignMinutes int    `mapstructure:"presign_minutes"`
}

type RedisConfig struct {
	Addr                  string `mapstructure:"addr"`
	Password              string `mapstructure:"password"`
	DB                    int    `mapstructure:"db"`
	Stream                string `mapstructure:"stream"`
	Group                 string `mapstructure:"group"`
	Consumer              string `mapstructure:"consumer"`
	VisibilityTimeoutSecs int    `mapstructure:"visibility_timeout_secs"`
	MaxAttempts           int    `mapstructure:"max_attempts"`
	RetryBaseDelaySecs    int    `mapstructure:"retry_base_delay_secs"`
	JobTimeoutSecs        int    `mapstructure:"job_timeout_secs"`
	ClaimIntervalSecs     int    `mapstructure:"claim_interval_secs"`
	ReadBlockMillis       int    `mapstructure:"read_block_millis"`
}

type OCRConfig struct {
	Provider             string `mapstructure:"provider"`
	Endpoint             string `mapstructure:"endpoint"`
	AccessKeyID          string `mapstructure:"access_key_id"`
	AccessKeySecret      string `mapstructure:"access_key_secret"`
	APIKey               string `mapstructure:"api_key"`
	TimeoutSecs          int    `mapstructure:"timeout_secs"`
	MaxRetries           int    `mapstructure:"max_retries"`
	RetryBaseDelayMillis int    `mapstructure:"retry_base_delay_millis"`
}

type SearchConfig struct {
	Provider             string `mapstructure:"provider"`
	Endpoint             string `mapstructure:"endpoint"`
	APIKey               string `mapstructure:"api_key"`
	TimeoutSecs          int    `mapstructure:"timeout_secs"`
	MaxRetries           int    `mapstructure:"max_retries"`
	RetryBaseDelayMillis int    `mapstructure:"retry_base_delay_millis"`
	MaxToolCalls         int    `mapstructure:"max_tool_calls"`
}

type LLMConfig struct {
	Provider              string `mapstructure:"provider"`
	Endpoint              string `mapstructure:"endpoint"`
	APIKey                string `mapstructure:"api_key"`
	Model                 string `mapstructure:"model"`
	TimeoutSecs           int    `mapstructure:"timeout_secs"`
	MaxRetries            int    `mapstructure:"max_retries"`
	RetryBaseDelayMillis  int    `mapstructure:"retry_base_delay_millis"`
	MaxTokens             int    `mapstructure:"max_tokens"`
	StreamIdleTimeoutSecs int    `mapstructure:"stream_idle_timeout_secs"`
}

// Validate 在启动前检查真实 LLM 的必要配置，避免服务启动后首个任务才发现无法认证。
func (c LLMConfig) Validate() error {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	if provider == "" || provider == "demo" {
		return nil
	}
	if provider != "http" && provider != "deepseek" {
		return fmt.Errorf("unsupported llm provider %q; use demo, http, or deepseek", c.Provider)
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return fmt.Errorf("llm endpoint is required for provider %q", provider)
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("llm model is required for provider %q", provider)
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("llm api key is required; set APP_LLM_API_KEY or DEEPSEEK_API_KEY before starting the server")
	}
	return nil
}

// RequiresAPIKey 标识当前 LLM Provider 是否必须在启动时获取密钥。
func (c LLMConfig) RequiresAPIKey() bool {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	return provider == "http" || provider == "deepseek"
}

// Load 统一读取 YAML 配置，并允许 APP_ 前缀环境变量覆盖文件配置，便于不同部署环境复用同一份配置模板。
func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.SetDefault("server.http_addr", ":8080")
	v.SetDefault("database.mysql_dsn", "")
	v.SetDefault("object_store.presign_minutes", 15)
	v.SetDefault("ocr.provider", "demo")
	v.SetDefault("ocr.timeout_secs", 30)
	v.SetDefault("ocr.max_retries", 2)
	v.SetDefault("ocr.retry_base_delay_millis", 200)
	v.SetDefault("search.provider", "noop")
	v.SetDefault("search.timeout_secs", 10)
	v.SetDefault("search.max_retries", 1)
	v.SetDefault("search.retry_base_delay_millis", 200)
	v.SetDefault("search.max_tool_calls", 3)
	v.SetDefault("llm.provider", "demo")
	// 讲解模型可能需要较长时间处理几何证明；默认值应覆盖正常慢响应，但仍小于队列作业超时。
	v.SetDefault("llm.timeout_secs", 90)
	v.SetDefault("llm.max_retries", 2)
	v.SetDefault("llm.retry_base_delay_millis", 200)
	v.SetDefault("llm.max_tokens", 8192)
	v.SetDefault("llm.stream_idle_timeout_secs", 30)
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 显式绑定嵌套字段，确保环境变量在 YAML 没有对应键时也能被 Unmarshal 读取。
	if err := v.BindEnv("server.http_addr", "APP_SERVER_HTTP_ADDR"); err != nil {
		return Config{}, fmt.Errorf("bind server.http_addr: %w", err)
	}
	if err := v.BindEnv("database.mysql_dsn", "APP_DATABASE_MYSQL_DSN"); err != nil {
		return Config{}, fmt.Errorf("bind database.mysql_dsn: %w", err)
	}
	for key, env := range map[string]string{
		"object_store.endpoint":        "APP_OBJECT_STORE_ENDPOINT",
		"object_store.region":          "APP_OBJECT_STORE_REGION",
		"object_store.bucket":          "APP_OBJECT_STORE_BUCKET",
		"object_store.access_key":      "APP_OBJECT_STORE_ACCESS_KEY",
		"object_store.secret_key":      "APP_OBJECT_STORE_SECRET_KEY",
		"object_store.use_path_style":  "APP_OBJECT_STORE_USE_PATH_STYLE",
		"object_store.key_prefix":      "APP_OBJECT_STORE_KEY_PREFIX",
		"object_store.presign_minutes": "APP_OBJECT_STORE_PRESIGN_MINUTES",
	} {
		if err := v.BindEnv(key, env); err != nil {
			return Config{}, fmt.Errorf("bind %s: %w", key, err)
		}
	}
	for key, env := range map[string]string{
		"llm.provider":                 "APP_LLM_PROVIDER",
		"llm.endpoint":                 "APP_LLM_ENDPOINT",
		"llm.model":                    "APP_LLM_MODEL",
		"llm.timeout_secs":             "APP_LLM_TIMEOUT_SECS",
		"llm.max_retries":              "APP_LLM_MAX_RETRIES",
		"llm.retry_base_delay_millis":  "APP_LLM_RETRY_BASE_DELAY_MILLIS",
		"llm.max_tokens":               "APP_LLM_MAX_TOKENS",
		"llm.stream_idle_timeout_secs": "APP_LLM_STREAM_IDLE_TIMEOUT_SECS",
	} {
		if err := v.BindEnv(key, env); err != nil {
			return Config{}, fmt.Errorf("bind %s: %w", key, err)
		}
	}
	// 同时兼容 DeepSeek 官方示例中的变量名，便于使用“一条命令临时注入”方式启动。
	if err := v.BindEnv("llm.api_key", "APP_LLM_API_KEY", "DEEPSEEK_API_KEY"); err != nil {
		return Config{}, fmt.Errorf("bind llm.api_key: %w", err)
	}
	for key, env := range map[string]string{
		"search.provider":                "APP_SEARCH_PROVIDER",
		"search.endpoint":                "APP_SEARCH_ENDPOINT",
		"search.api_key":                 "APP_SEARCH_API_KEY",
		"search.timeout_secs":            "APP_SEARCH_TIMEOUT_SECS",
		"search.max_retries":             "APP_SEARCH_MAX_RETRIES",
		"search.retry_base_delay_millis": "APP_SEARCH_RETRY_BASE_DELAY_MILLIS",
		"search.max_tool_calls":          "APP_SEARCH_MAX_TOOL_CALLS",
	} {
		if err := v.BindEnv(key, env); err != nil {
			return Config{}, fmt.Errorf("bind %s: %w", key, err)
		}
	}
	for key, env := range map[string]string{
		"ocr.provider":                "APP_OCR_PROVIDER",
		"ocr.endpoint":                "APP_OCR_ENDPOINT",
		"ocr.access_key_id":           "APP_OCR_ACCESS_KEY_ID",
		"ocr.access_key_secret":       "APP_OCR_ACCESS_KEY_SECRET",
		"ocr.api_key":                 "APP_OCR_API_KEY",
		"ocr.timeout_secs":            "APP_OCR_TIMEOUT_SECS",
		"ocr.max_retries":             "APP_OCR_MAX_RETRIES",
		"ocr.retry_base_delay_millis": "APP_OCR_RETRY_BASE_DELAY_MILLIS",
	} {
		if err := v.BindEnv(key, env); err != nil {
			return Config{}, fmt.Errorf("bind %s: %w", key, err)
		}
	}
	for key, env := range map[string]string{
		"redis.addr":                    "APP_REDIS_ADDR",
		"redis.password":                "APP_REDIS_PASSWORD",
		"redis.db":                      "APP_REDIS_DB",
		"redis.stream":                  "APP_REDIS_STREAM",
		"redis.group":                   "APP_REDIS_GROUP",
		"redis.consumer":                "APP_REDIS_CONSUMER",
		"redis.visibility_timeout_secs": "APP_REDIS_VISIBILITY_TIMEOUT_SECS",
		"redis.max_attempts":            "APP_REDIS_MAX_ATTEMPTS",
		"redis.retry_base_delay_secs":   "APP_REDIS_RETRY_BASE_DELAY_SECS",
		"redis.job_timeout_secs":        "APP_REDIS_JOB_TIMEOUT_SECS",
		"redis.claim_interval_secs":     "APP_REDIS_CLAIM_INTERVAL_SECS",
		"redis.read_block_millis":       "APP_REDIS_READ_BLOCK_MILLIS",
	} {
		if err := v.BindEnv(key, env); err != nil {
			return Config{}, fmt.Errorf("bind %s: %w", key, err)
		}
	}

	if path := os.Getenv("APP_CONFIG_FILE"); path != "" {
		v.SetConfigFile(path)
	} else {
		v.AddConfigPath(".")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
