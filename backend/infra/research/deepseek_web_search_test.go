package research

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/research"
)

func TestDeepSeekWebSearchAdapterRunsToolCallingLoop(t *testing.T) {
	deepSeekCalls := 0
	searchCalls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "deepseek.test" {
			deepSeekCalls++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if deepSeekCalls == 1 && payload["tool_choice"] != "required" {
				return nil, fmt.Errorf("first call must require web_search, got %#v", payload["tool_choice"])
			}
			thinking, ok := payload["thinking"].(map[string]any)
			if !ok || thinking["type"] != "disabled" {
				return nil, fmt.Errorf("tool calling must disable thinking mode, got %#v", payload["thinking"])
			}
			if deepSeekCalls == 1 {
				return jsonResponse(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"2020福建原题检索\"}"}}]}}]}`), nil
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) < 4 {
				return nil, fmt.Errorf("second call must include tool result, messages=%#v", payload["messages"])
			}
			responseFormat, ok := payload["response_format"].(map[string]any)
			if !ok || responseFormat["type"] != "json_object" {
				return nil, fmt.Errorf("final call must request JSON output, got %#v", payload["response_format"])
			}
			return jsonResponse(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"status\":\"verified\",\"matchedQuestion\":{\"title\":\"原题\",\"matchScore\":0.95,\"matchedSubquestionCount\":2,\"checks\":[\"题干匹配\"]},\"referenceSolution\":{\"status\":\"candidate\",\"steps\":[\"步骤一\"],\"finalAnswer\":\"答案\"}}"}}]}`), nil
		}
		if r.URL.Host == "search.test" {
			searchCalls++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload["query"] != "2020福建原题检索" || payload["count"] != float64(5) || payload["need_content"] != true || r.Header.Get("Authorization") != "Bearer search-key" {
				return nil, fmt.Errorf("unexpected search request: %#v", payload)
			}
			return jsonResponse(`{"results":[{"title":"原题来源","url":"https://example.com/problem","snippet":"短摘要","summary":"2020福建原题检索题干和解法摘要","content":"网页正文","rerank_score":2.4,"authority":3}]}`), nil
		}
		return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
	})

	adapter := NewDeepSeekWebSearchAdapter(DeepSeekWebSearchConfig{
		DeepSeekEndpoint: "http://deepseek.test",
		DeepSeekAPIKey:   "deepseek-key",
		DeepSeekModel:    "deepseek-chat",
		SearchEndpoint:   "http://search.test",
		SearchAPIKey:     "search-key",
		Timeout:          time.Second,
		MaxToolCalls:     2,
	})
	adapter.client.Transport = transport
	result, err := adapter.Search(context.Background(), "2020福建原题检索")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if deepSeekCalls != 1 || searchCalls != 1 || result.Status != "verified" || len(result.Sources) != 1 || result.MatchedQuestion == nil {
		t.Fatalf("unexpected research result: DeepSeek calls=%d search calls=%d result=%+v", deepSeekCalls, searchCalls, result)
	}
}

func TestNormalizeAnswerPreservesSourcesWhenAgentOutputIsInvalid(t *testing.T) {
	sources := []domain.Source{{Title: "原题来源", URL: "https://example.com", Summary: "2020福建原题检索搜索摘要", TrustScore: 0.9}}
	adapter := NewDeepSeekWebSearchAdapter(DeepSeekWebSearchConfig{})
	result, err := adapter.normalizeAnswer("2020福建原题检索", sources, "不是 JSON")
	if err != nil || result.ErrorCode != "invalid_agent_output" || len(result.Sources) != 1 || result.Status != domain.StatusVerified {
		t.Fatalf("invalid output must preserve sources and error code: result=%+v err=%v", result, err)
	}
}

func TestDeepSeekWebSearchAdapterRejectsMissingConfiguration(t *testing.T) {
	adapter := NewDeepSeekWebSearchAdapter(DeepSeekWebSearchConfig{Timeout: time.Second})
	result, err := adapter.Search(context.Background(), "题目")
	if err == nil || result.Status != "research_unavailable" || result.ErrorCode != "deepseek_not_configured" {
		t.Fatalf("expected explicit configuration failure, result=%+v err=%v", result, err)
	}
	if strings.TrimSpace(result.Query) != "题目" {
		t.Fatalf("query should be preserved for retry diagnostics: %+v", result)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
