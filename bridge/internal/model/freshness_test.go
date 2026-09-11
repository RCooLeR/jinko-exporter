package model

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestValidateCollectionTime(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		collectedAt time.Time
		maxAge      time.Duration
		wantErr     error
	}{
		{name: "fresh", collectedAt: now.Add(-time.Minute)},
		{name: "age boundary", collectedAt: now.Add(-DefaultMaxDataAge)},
		{name: "default expired", collectedAt: now.Add(-DefaultMaxDataAge - time.Nanosecond), wantErr: ErrStaleCollectionTime},
		{name: "two days old", collectedAt: now.Add(-48 * time.Hour), wantErr: ErrStaleCollectionTime},
		{name: "configured shorter window", collectedAt: now.Add(-2 * time.Minute), maxAge: time.Minute, wantErr: ErrStaleCollectionTime},
		{name: "configured longer window", collectedAt: now.Add(-20 * time.Minute), maxAge: 30 * time.Minute},
		{name: "clock skew boundary", collectedAt: now.Add(MaxCollectionFutureSkew)},
		{name: "future", collectedAt: now.Add(MaxCollectionFutureSkew + time.Nanosecond), wantErr: ErrFutureCollectionTime},
		{name: "missing", wantErr: ErrInvalidCollectionTime},
		{name: "epoch", collectedAt: time.Unix(0, 0), wantErr: ErrInvalidCollectionTime},
		{name: "negative epoch", collectedAt: time.Unix(-1, 0), wantErr: ErrInvalidCollectionTime},
		{name: "out of range", collectedAt: time.Unix(math.MaxInt64, 0), wantErr: ErrInvalidCollectionTime},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectionTime(tt.collectedAt, now, tt.maxAge)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateCollectionTime() = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if err := ValidateCollectionTime(now, now, -time.Minute); err == nil {
		t.Fatal("negative maximum age disabled freshness validation")
	}
}
