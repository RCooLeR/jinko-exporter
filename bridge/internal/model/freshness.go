package model

import (
	"errors"
	"time"
)

const DefaultMaxDataAge = 15 * time.Minute
const MaxCollectionFutureSkew = time.Minute

var (
	ErrInvalidCollectionTime = errors.New("collection time is missing or invalid")
	ErrFutureCollectionTime  = errors.New("collection time is too far in the future")
	ErrStaleCollectionTime   = errors.New("collection time exceeds maximum data age")
)

// ValidateCollectionTime checks the device's measurement time, not the time of
// the HTTP response. A cached cloud response must expire even when repeated API
// requests succeed. Zero maxAge retains a safe default for programmatic callers.
func ValidateCollectionTime(collectedAt, now time.Time, maxAge time.Duration) error {
	if maxAge == 0 {
		maxAge = DefaultMaxDataAge
	}
	if maxAge < 0 {
		return errors.New("maximum data age must be positive")
	}
	if collectedAt.IsZero() || collectedAt.Unix() <= 0 || collectedAt.Year() > 9999 {
		return ErrInvalidCollectionTime
	}
	if collectedAt.After(now.Add(MaxCollectionFutureSkew)) {
		return ErrFutureCollectionTime
	}
	if now.Sub(collectedAt) > maxAge {
		return ErrStaleCollectionTime
	}
	return nil
}
