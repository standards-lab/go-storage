package storage

import (
	"cmp"
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
		return 0, r.stalled(context.DeadlineExceeded)
	}
	r.timer.Reset(r.limit)
	n, err := r.body.Read(p)
	if !r.timer.Stop() && r.fired.Load() {
		// The timer fired as the read returned: the request is cancelled,
		// so a read that did not finish the body is the stall's.
		if err == io.EOF {
			return n, err
		}
		return n, r.stalled(cmp.Or(err, context.DeadlineExceeded))
	}
	return n, err
}

// stalled is the error of a read the timer cut off. The cause is kept as
// text only: it is the cancellation the timer made, which a caller must
// not read as its own context's.
func (r *idleReader) stalled(cause error) error {
	return fmt.Errorf("%w: no bytes read within the read idle timeout (%s): %v", ErrUnavailable, r.limit, cause)
}

// Close cancels the request before it closes the body, so a provider's
// body that resumes after a failed read sees the request done and stops.
func (r *idleReader) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.body.Close()
}
