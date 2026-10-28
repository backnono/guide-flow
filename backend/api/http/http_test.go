package http_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	apihttp "github.com/guide-flow/guide-flow/backend/api/http"
	"github.com/guide-flow/guide-flow/backend/application/lesson"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	"github.com/guide-flow/guide-flow/backend/infra/ai"
	"github.com/guide-flow/guide-flow/backend/infra/objectstore"
	"github.com/guide-flow/guide-flow/backend/infra/persistence/memory"
	"github.com/guide-flow/guide-flow/backend/infra/queue"
)

func TestLessonImageAPIStreamsOriginalImage(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	service := lesson.NewService(repo, queue.NewQueue(1), images)
	created, err := service.CreateLesson(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png", domain.LearnerProfile{Grade: 5})
	if err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/lessons/"+created.ID+"/image", nil)
	apihttp.NewHandler(service, ai.DemoOCR{}).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || resp.Header().Get("Content-Type") != "image/png" || resp.Body.String() != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("unexpected image response: status=%d type=%q body=%q", resp.Code, resp.Header().Get("Content-Type"), resp.Body.String())
	}
}

func TestCreateLessonAPI(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := queue.NewQueue(1)
	service := lesson.NewService(repo, queue, images)
	requestBody := &strings.Builder{}
	writer := multipart.NewWriter(requestBody)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="image"; filename="question.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "image")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/lessons", strings.NewReader(requestBody.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()
	apihttp.NewHandler(service, ai.DemoOCR{}).ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAPIErrorContainsStableContractAndRequestID(t *testing.T) {
	repo := memory.NewLessonRepository()
	service := lesson.NewService(repo, queue.NewQueue(1), objectstore.NewMemoryImageStore())
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/lessons/missing", nil)
	req.Header.Set("X-Request-ID", "request-test-1")
	apihttp.NewHandler(service, ai.DemoOCR{}).ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("X-Request-ID") != "request-test-1" {
		t.Fatalf("request id header was not preserved: %q", resp.Header().Get("X-Request-ID"))
	}
	body := resp.Body.String()
	for _, expected := range []string{`"code":"lesson_not_found"`, `"requestId":"request-test-1"`, `"lessonId":"missing"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("error response does not contain %q: %s", expected, body)
		}
	}
}

func TestCreateLessonSupportsLearnerProfileAndIdempotency(t *testing.T) {
	repo := memory.NewLessonRepository()
	service := lesson.NewService(repo, queue.NewQueue(2), objectstore.NewMemoryImageStore())
	handler := apihttp.NewHandler(service, ai.DemoOCR{})

	first := httptest.NewRecorder()
	requestBody, contentType := createMultipartBody(t, `{"grade":6}`)
	req := httptest.NewRequest(http.MethodPost, "/api/lessons", requestBody)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", "same-request")
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"grade":6`) {
		t.Fatalf("unexpected first response: %d %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	requestBody, contentType = createMultipartBody(t, `{"grade":5}`)
	req = httptest.NewRequest(http.MethodPost, "/api/lessons", requestBody)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", "same-request")
	handler.ServeHTTP(second, req)
	if second.Code != http.StatusAccepted || !strings.Contains(second.Body.String(), `"grade":6`) {
		t.Fatalf("idempotency key did not return original lesson: %d %s", second.Code, second.Body.String())
	}
}

func createMultipartBody(t *testing.T, profile string) (*strings.Reader, string) {
	t.Helper()
	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", "question.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "image")
	if err = writer.WriteField("learnerProfile", profile); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(body.String()), writer.FormDataContentType()
}
