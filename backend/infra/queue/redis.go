package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrRedisQueueClosed = errors.New("redis queue is closed")

type RedisConfig struct {
	Addr              string
	Password          string
	DB                int
	Stream            string
	Group             string
	Consumer          string
	VisibilityTimeout time.Duration
	MaxAttempts       int
	RetryBaseDelay    time.Duration
	JobTimeout        time.Duration
	ClaimInterval     time.Duration
	ReadBlock         time.Duration
}

type RedisQueue struct {
	client redis.UniversalClient
	config RedisConfig
}

func NewRedisQueue(ctx context.Context, cfg RedisConfig) (*RedisQueue, error) {
	if cfg.Addr == "" {
		return nil, errors.New("redis address is required")
	}
	if cfg.Stream == "" {
		cfg.Stream = "guide-flow:lessons"
	}
	if cfg.Group == "" {
		cfg.Group = "guide-flow-workers"
	}
	if cfg.Consumer == "" {
		cfg.Consumer = "worker-1"
	}
	if cfg.VisibilityTimeout <= 0 {
		cfg.VisibilityTimeout = 30 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 2 * time.Second
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 2 * time.Minute
	}
	if cfg.ClaimInterval <= 0 {
		cfg.ClaimInterval = 5 * time.Second
	}
	if cfg.ReadBlock <= 0 {
		cfg.ReadBlock = 2 * time.Second
	}
	client := redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB})
	queue := &RedisQueue{client: client, config: cfg}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	if err := queue.ensureGroup(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return queue, nil
}

func (q *RedisQueue) Enqueue(lessonID string) error {
	if lessonID == "" {
		return errors.New("lesson id is required")
	}
	jobID := uuid.NewString()
	dedupeKey := q.config.Stream + ":lesson-pending:" + lessonID
	accepted, err := q.client.SetNX(context.Background(), dedupeKey, jobID, q.config.JobTimeout+q.config.VisibilityTimeout).Result()
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	if err := q.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: q.config.Stream,
		Values: map[string]interface{}{"job_id": jobID, "lesson_id": lessonID},
	}).Err(); err != nil {
		_ = q.client.Del(context.Background(), dedupeKey).Err()
		return err
	}
	return nil
}

func (q *RedisQueue) Run(ctx context.Context, process Processor) {
	claimTicker := time.NewTicker(q.config.ClaimInterval)
	defer claimTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-claimTicker.C:
			q.claimPending(ctx, process)
		default:
			if q.readNew(ctx, process) {
				return
			}
		}
	}
}

func (q *RedisQueue) Close() error {
	if q.client == nil {
		return ErrRedisQueueClosed
	}
	return q.client.Close()
}

func (q *RedisQueue) ensureGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.config.Stream, q.config.Group, "0").Err()
	if err != nil && !isBusyGroupError(err) {
		return fmt.Errorf("create redis consumer group: %w", err)
	}
	return nil
}

func (q *RedisQueue) readNew(ctx context.Context, process Processor) bool {
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: q.config.Group, Consumer: q.config.Consumer, Streams: []string{q.config.Stream, ">"}, Count: 1, Block: q.config.ReadBlock,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return ctx.Err() != nil
	}
	for _, stream := range streams {
		for _, message := range stream.Messages {
			q.consume(ctx, message, false, process)
		}
	}
	return false
}

func (q *RedisQueue) claimPending(ctx context.Context, process Processor) {
	messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: q.config.Stream, Group: q.config.Group, Consumer: q.config.Consumer,
		MinIdle: q.config.VisibilityTimeout, Start: "0-0", Count: 10,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return
	}
	for _, message := range messages {
		q.consume(ctx, message, true, process)
	}
}

func (q *RedisQueue) consume(ctx context.Context, message redis.XMessage, reclaimed bool, process Processor) {
	jobID, lessonID, ok := messageFields(message.Values)
	if !ok {
		_ = q.client.XAck(ctx, q.config.Stream, q.config.Group, message.ID).Err()
		return
	}
	lockKey := q.config.Stream + ":lock:" + jobID
	token := uuid.NewString()
	locked, err := q.client.SetNX(ctx, lockKey, token, q.config.JobTimeout+q.config.VisibilityTimeout).Result()
	if err != nil || !locked {
		return
	}
	defer q.releaseLock(ctx, lockKey, token)
	attempt, due := q.nextAttempt(ctx, jobID, reclaimed)
	if err != nil || !due {
		return
	}
	if attempt > q.config.MaxAttempts {
		_ = q.client.XAck(ctx, q.config.Stream, q.config.Group, message.ID).Err()
		q.clearAttempt(ctx, jobID)
		q.clearLessonPending(ctx, lessonID)
		return
	}
	job := Job{ID: jobID, LessonID: lessonID, Attempt: attempt}
	jobCtx, cancel := context.WithTimeout(ctx, q.config.JobTimeout)
	err = process(jobCtx, job)
	cancel()
	if err == nil {
		_ = q.client.XAck(ctx, q.config.Stream, q.config.Group, message.ID).Err()
		q.clearAttempt(ctx, jobID)
		q.clearLessonPending(ctx, lessonID)
		return
	}
	if attempt >= q.config.MaxAttempts {
		// 达到上限后确认消息，Lesson 已由 Worker 持久化为 failed，等待人工重试。
		_ = q.client.XAck(ctx, q.config.Stream, q.config.Group, message.ID).Err()
		q.clearAttempt(ctx, jobID)
		q.clearLessonPending(ctx, lessonID)
		return
	}
	// 不 ACK，让消息留在 Pending；超时回收后再次执行。next_retry_at 提供指数退避，避免立即重试打满外部依赖。
	next := time.Now().Add(retryDelay(q.config.RetryBaseDelay, attempt))
	_ = q.client.HSet(ctx, q.attemptKey(jobID), "next_retry_at", next.UnixMilli()).Err()
}

func (q *RedisQueue) nextAttempt(ctx context.Context, jobID string, reclaimed bool) (int, bool) {
	key := q.attemptKey(jobID)
	values, err := q.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	attempt, _ := strconv.Atoi(values["attempt"])
	next, _ := strconv.ParseInt(values["next_retry_at"], 10, 64)
	if next > time.Now().UnixMilli() {
		return attempt, false
	}
	if attempt == 0 {
		attempt = 1
	} else if reclaimed {
		attempt++
	}
	if err = q.client.HSet(ctx, key, "attempt", attempt, "next_retry_at", 0).Err(); err != nil {
		return 0, false
	}
	return attempt, true
}

func (q *RedisQueue) attemptKey(jobID string) string { return q.config.Stream + ":attempt:" + jobID }

func (q *RedisQueue) clearAttempt(ctx context.Context, jobID string) {
	_ = q.client.Del(ctx, q.attemptKey(jobID)).Err()
}

func (q *RedisQueue) clearLessonPending(ctx context.Context, lessonID string) {
	_ = q.client.Del(ctx, q.config.Stream+":lesson-pending:"+lessonID).Err()
}

func (q *RedisQueue) releaseLock(ctx context.Context, key, token string) {
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	_ = q.client.Eval(ctx, script, []string{key}, token).Err()
}

func messageFields(values map[string]interface{}) (string, string, bool) {
	toString := func(value interface{}) string {
		switch typed := value.(type) {
		case string:
			return typed
		case []byte:
			return string(typed)
		default:
			return fmt.Sprint(typed)
		}
	}
	jobID, lessonID := toString(values["job_id"]), toString(values["lesson_id"])
	return jobID, lessonID, jobID != "" && lessonID != ""
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if attempt <= 1 {
		return base
	}
	for i := 1; i < attempt && base < 30*time.Second; i++ {
		base *= 2
	}
	if base > 30*time.Second {
		return 30 * time.Second
	}
	return base
}

func isBusyGroupError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}
