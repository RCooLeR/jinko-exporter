package alert

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingNotifier struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (n *blockingNotifier) Notify(ctx context.Context, _, _ string) error {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
	select {
	case n.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-n.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (n *blockingNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

func TestManagerReservesConcurrentDeliveryByKey(t *testing.T) {
	notifier := &blockingNotifier{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	manager := NewManager(notifier, time.Hour)
	event := Event{Key: "same", Subject: "subject", Body: "body"}

	firstDone := make(chan bool, 1)
	go func() {
		firstDone <- manager.NotifyDelivered(context.Background(), event)
	}()
	select {
	case <-notifier.entered:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not enter notifier")
	}

	if manager.NotifyDelivered(context.Background(), event) {
		t.Fatal("concurrent duplicate reported delivery")
	}
	if got := notifier.callCount(); got != 1 {
		t.Fatalf("notifier calls = %d, want 1 while key is reserved", got)
	}

	close(notifier.release)
	select {
	case delivered := <-firstDone:
		if !delivered {
			t.Fatal("first delivery reported failure")
		}
	case <-time.After(time.Second):
		t.Fatal("first delivery did not finish")
	}
}

type sequenceNotifier struct {
	mu      sync.Mutex
	results []error
	calls   int
}

func (n *sequenceNotifier) Notify(context.Context, string, string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if len(n.results) == 0 {
		return nil
	}
	err := n.results[0]
	n.results = n.results[1:]
	return err
}

func TestManagerRetriesAfterDeliveryFailure(t *testing.T) {
	notifier := &sequenceNotifier{results: []error{errors.New("temporary failure"), nil}}
	manager := NewManager(notifier, time.Hour)
	event := Event{Key: "retry", Subject: "subject", Body: "body"}

	if manager.NotifyDelivered(context.Background(), event) {
		t.Fatal("failed delivery reported success")
	}
	if !manager.NotifyDelivered(context.Background(), event) {
		t.Fatal("retry after failed delivery was suppressed")
	}
	if notifier.calls != 2 {
		t.Fatalf("notifier calls = %d, want 2", notifier.calls)
	}
}

func TestManagerRetriesFailedRecoveryDelivery(t *testing.T) {
	notifier := &sequenceNotifier{results: []error{nil, errors.New("temporary recovery failure"), nil}}
	manager := NewManager(notifier, 0)
	active := Event{Key: "active", Subject: "active", Body: "active"}
	recovery := Event{Key: "recovered", Subject: "recovered", Body: "recovered"}

	manager.NotifyActive(context.Background(), active)
	manager.Resolve(context.Background(), active.Key, recovery, true)
	manager.Resolve(context.Background(), active.Key, recovery, true)

	if notifier.calls != 3 {
		t.Fatalf("notifier calls = %d, want active + failed recovery + retry", notifier.calls)
	}
}
