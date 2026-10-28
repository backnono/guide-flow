package queue

import (
	"context"
	"fmt"
)

type Job struct {
	ID       string
	LessonID string
	Attempt  int
}

type Processor func(context.Context, Job) error

type Queue struct{ jobs chan Job }

func NewQueue(size int) *Queue { return &Queue{jobs: make(chan Job, size)} }

func (q *Queue) Enqueue(id string) error {
	q.jobs <- Job{ID: fmt.Sprintf("memory-%s", id), LessonID: id}
	return nil
}

func (q *Queue) Run(ctx context.Context, process Processor) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-q.jobs:
			_ = process(ctx, job)
		}
	}
}
