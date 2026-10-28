package research

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPAdapterParsesResearchContext(t *testing.T) {
	adapter := NewHTTPAdapter(HTTPConfig{Endpoint: "http://search.test", Timeout: time.Second})
	adapter.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"sources":[{"title":"教材来源","url":"https://example.com","summary":"摘要","trustScore":0.9}],"status":"used"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	result, err := adapter.Search(context.Background(), "矩形垂直")
	if err != nil || result.Status != "used" || result.Query != "矩形垂直" || len(result.Sources) != 1 {
		t.Fatalf("unexpected result: %+v, err=%v", result, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
