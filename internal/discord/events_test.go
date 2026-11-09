package discord

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestEventQueueOrderAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := newEventQueue(ctx)
	started, release := make(chan struct{}), make(chan struct{})
	var order []string
	queue.enqueue(ctx, func() {
		close(started)
		<-release
		order = append(order, "message")
	})
	<-started

	enqueued := make(chan struct{})
	complete := make(chan struct{})
	go func() {
		queue.enqueue(ctx, func() { order = append(order, "reaction"); close(complete) })
		close(enqueued)
	}()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("enqueue blocked behind network processing")
	}
	close(release)
	select {
	case <-complete:
	case <-time.After(time.Second):
		t.Fatal("queue did not process reaction")
	}
	if !reflect.DeepEqual(order, []string{"message", "reaction"}) {
		t.Fatalf("event order: %v", order)
	}

	cancel()
	select {
	case <-queue.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	queue.enqueue(ctx, func() { t.Error("processed event after cancellation") })
}
