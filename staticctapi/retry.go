package staticctapi

import (
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var DefaultTileRetry = Retry{
	MaxAttempts: 5,
	MaxInterval: 1 * time.Second,
	Timeout:     5 * time.Second,
}

type Retry struct {
	// MaxAttempts is the maximum number of times to attempt a request before
	// giving up.
	MaxAttempts int

	// MaxInterval is the maximum time to wait between retries.
	MaxInterval time.Duration

	// Timeout is the maximum time to spend on a request, including retries.
	Timeout time.Duration
}

func (r Retry) Validate() error {
	if r.MaxAttempts < 1 {
		return errors.New("max attempts less than one")
	}

	if r.MaxInterval <= 0 {
		return errors.New("max interval less than or equal to zero")
	}

	if r.Timeout <= 0 {
		return errors.New("timeout less than or equal to zero")
	}

	if r.Timeout <= r.MaxInterval {
		return errors.New("timeout less than or equal to max interval")
	}

	return nil
}

func (r Retry) createBackoff() backoff.BackOff {
	var bo backoff.BackOff = backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(r.Timeout),
		backoff.WithMaxInterval(r.MaxInterval),
	)
	return backoff.WithMaxRetries(bo, uint64(r.MaxAttempts)-1)
}
