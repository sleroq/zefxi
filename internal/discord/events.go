package discord

import "context"

type eventQueue struct {
	jobs chan func()
	done chan struct{}
}

func newEventQueue(ctx context.Context) *eventQueue {
	queue := &eventQueue{jobs: make(chan func(), 256), done: make(chan struct{})}
	go queue.run(ctx)
	return queue
}

func (q *eventQueue) enqueue(ctx context.Context, job func()) {
	select {
	case q.jobs <- job:
	case <-ctx.Done():
	}
}

func (q *eventQueue) run(ctx context.Context) {
	defer close(q.done)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-q.jobs:
			if ctx.Err() != nil {
				return
			}
			job()
		}
	}
}
