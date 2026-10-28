package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsYAMLAndEnvironmentOverrides(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte("server:\n  http_addr: ':9000'\ndatabase:\n  mysql_dsn: 'yaml-dsn'\nobject_store:\n  endpoint: 'http://localhost:9000'\n  bucket: 'guide-flow'\n  use_path_style: true\nllm:\n  provider: 'http'\n  endpoint: 'http://llm.yaml/v1/draft'\n  model: 'yaml-model'\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("APP_CONFIG_FILE", configFile)
	t.Setenv("APP_SERVER_HTTP_ADDR", ":9100")
	t.Setenv("APP_DATABASE_MYSQL_DSN", "env-dsn")
	t.Setenv("APP_LLM_ENDPOINT", "http://llm.env/v1/draft")
	t.Setenv("APP_LLM_MODEL", "env-model")
	t.Setenv("APP_LLM_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.HTTPAddr != ":9100" {
		t.Fatalf("unexpected HTTP address: %q", cfg.Server.HTTPAddr)
	}
	if cfg.Database.MySQLDSN != "env-dsn" {
		t.Fatalf("unexpected MySQL DSN: %q", cfg.Database.MySQLDSN)
	}
	if cfg.ObjectStore.Endpoint != "http://localhost:9000" || cfg.ObjectStore.Bucket != "guide-flow" || !cfg.ObjectStore.UsePathStyle {
		t.Fatalf("unexpected object store config: %+v", cfg.ObjectStore)
	}
	if cfg.LLM.Provider != "http" || cfg.LLM.Endpoint != "http://llm.env/v1/draft" || cfg.LLM.Model != "env-model" || cfg.LLM.APIKey != "test-key" {
		t.Fatalf("unexpected LLM config: %+v", cfg.LLM)
	}
}

func TestLLMConfigRequiresAPIKeyForRealProvider(t *testing.T) {
	err := (LLMConfig{
		Provider: "deepseek",
		Endpoint: "https://api.deepseek.com/chat/completions",
		Model:    "deepseek-flash",
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "APP_LLM_API_KEY") || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("expected startup API key guidance, got %v", err)
	}
}

func TestLoadAcceptsDeepSeekAPIKeyAlias(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte("llm:\n  provider: deepseek\n  endpoint: https://api.deepseek.com/chat/completions\n  model: deepseek-flash\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("APP_CONFIG_FILE", configFile)
	t.Setenv("DEEPSEEK_API_KEY", "alias-key")
	t.Setenv("APP_LLM_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LLM.APIKey != "alias-key" {
		t.Fatalf("expected DeepSeek API key alias, got %q", cfg.LLM.APIKey)
	}
}
