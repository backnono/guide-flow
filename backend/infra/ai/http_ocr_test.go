package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestHTTPOCRAdapterParsesFieldsAndKeepsRawResponse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("X-Request-ID") == "" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.ReadAll(r.Body)
		return response(http.StatusOK, `{"text":"120页","confidence":0.95,"fieldConfidence":{"numbers":0.7,"units":0.98}}`), nil
	})
	adapter := NewHTTPOCRAdapter(HTTPOCRConfig{Endpoint: "http://ocr.test", Timeout: time.Second})
	adapter.client.Transport = transport
	result, err := adapter.RecognizeWithMetadata(context.Background(), &byteReader{data: []byte("image")}, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if result.Draft.Text != "120页" || result.Draft.FieldConfidence["numbers"] != .7 || string(result.RawResponse) == "" {
		t.Fatalf("unexpected OCR result: %+v", result)
	}
}

func TestHTTPOCRAdapterRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return response(http.StatusBadGateway, "temporary"), nil
		}
		return response(http.StatusOK, `{"text":"题目","confidence":0.9}`), nil
	})
	adapter := NewHTTPOCRAdapter(HTTPOCRConfig{Endpoint: "http://ocr.test", MaxRetries: 1, RetryBaseDelay: time.Millisecond})
	adapter.client.Transport = transport
	if _, err := adapter.RecognizeWithMetadata(context.Background(), &byteReader{data: []byte("image")}, "a.png"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one retry, got %d calls", calls.Load())
	}
}

func TestHTTPOCRAdapterRejectsInvalidResponse(t *testing.T) {
	adapter := NewHTTPOCRAdapter(HTTPOCRConfig{Endpoint: "http://ocr.test"})
	adapter.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"text":"","confidence":1.2}`), nil
	})
	_, err := adapter.RecognizeWithMetadata(context.Background(), &byteReader{data: []byte("image")}, "a.png")
	if err == nil || !containsError(err, domain.ErrOCRInvalidResponse) {
		t.Fatalf("expected invalid response error, got %v", err)
	}
}

type byteReader struct{ data []byte }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func containsError(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
