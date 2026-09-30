package storage

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// idleReader bounds each read of a Get's body by limit. A timer runs only
// while a read is in progress; when it fires, it cancels the Get's
// request, which unblocks the provider's read, and the read reports
// [ErrUnavailable] with the provider's error as its cause. Close stops
// the timer and releases the request.
type idleReader struct {
	body   io.ReadCloser
	limit  time.Duration
	cancel context.CancelFunc
	timer  *time.Timer
	fired  atomic.Bool
}

func newIdleReader(body io.ReadCloser, limit time.Duration, cancel context.CancelFunc) *idleReader {
	r := &idleReader{body: body, limit: limit, cancel: cancel}
	r.timer = time.AfterFunc(limit, func() {
		r.fired.Store(true)
		cancel()
	})
	r.timer.Stop()
	return r
}

func (r *idleReader) Read(p []byte) (int, error) {
	if r.fired.Load() {
		return 0, r.stalled(context.Canceled)
	}
	r.timer.Reset(r.limit)
	n, err := r.body.Read(p)
	r.timer.Stop()
	if err != nil && err != io.EOF && r.fired.Load() {
		return n, r.stalled(err)
	}
	return n, err
}

func (r *idleReader) stalled(cause error) error {
	return fmt.Errorf("%w: no bytes read within the read idle timeout (%s): %w", ErrUnavailable, r.limit, cause)
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	err := r.body.Close()
	r.cancel()
	return err
}
