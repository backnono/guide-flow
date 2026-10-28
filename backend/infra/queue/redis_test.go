package queue

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryDelayIsExponentialAndCapped(t *testing.T) {
	if got := retryDelay(time.Second, 1); got != time.Second {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := retryDelay(time.Second, 3); got != 4*time.Second {
		t.Fatalf("attempt 3 delay = %s", got)
	}
	if got := retryDelay(time.Second, 10); got != 30*time.Second {
		t.Fatalf("capped delay = %s", got)
	}
}

func TestRedisQueueRetriesPendingJob(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR is not configured")
	}
	stream := "guide-flow:test:" + time.Now().Format("150405.000000000")
	queue, err := NewRedisQueue(context.Background(), RedisConfig{
		Addr: addr, Stream: stream, Group: "test-group", Consumer: "test-consumer",
		VisibilityTimeout: 80 * time.Millisecond, MaxAttempts: 3, RetryBaseDelay: 20 * time.Millisecond,
		JobTimeout: time.Second, ClaimInterval: 20 * time.Millisecond, ReadBlock: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = queue.client.Del(context.Background(), stream).Err()
		_ = queue.Close()
	}()
	if err = queue.Enqueue("lesson-retry"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	done := make(chan struct{})
	go queue.Run(ctx, func(_ context.Context, job Job) error {
		if job.LessonID != "lesson-retry" {
			t.Errorf("unexpected lesson id: %s", job.LessonID)
		}
		if calls.Add(1) == 1 {
			return context.DeadlineExceeded
		}
		close(done)
		return nil
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("redis queue did not retry pending job")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
}
