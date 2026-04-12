package source

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/model"
)

type fakeSource struct {
	name     string
	snapshot *model.Snapshot
	err      error
}

func (s fakeSource) Name() string {
	return s.name
}

func (s fakeSource) Fetch(context.Context) (*model.Snapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshot, nil
}

func TestPriorityFetchUsesFirstSuccessfulSource(t *testing.T) {
	expected := &model.Snapshot{
		Source:      "solarman",
		CollectedAt: time.Now(),
	}
	src := NewPriority([]Source{
		fakeSource{name: "jinko", err: errors.New("expired cert")},
		fakeSource{name: "solarman", snapshot: expected},
	})

	actual, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if actual != expected {
		t.Fatalf("Fetch() snapshot = %p, want %p", actual, expected)
	}
}

func TestPriorityFetchReturnsAllFailures(t *testing.T) {
	src := NewPriority([]Source{
		fakeSource{name: "jinko", err: errors.New("expired cert")},
		fakeSource{name: "solarman", err: errors.New("auth failed")},
	})

	_, err := src.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
	for _, want := range []string{"jinko", "expired cert", "solarman", "auth failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Fetch() error = %q, want to contain %q", err.Error(), want)
		}
	}
}
