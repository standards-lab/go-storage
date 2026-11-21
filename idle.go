package storage

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

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
		if err == io.EOF {
			return n, err
		}
		return n, r.stalled(cmp.Or(err, context.DeadlineExceeded))
	}
	return n, err
}

func (r *idleReader) stalled(cause error) error {
	return fmt.Errorf("%w: no bytes read within the read idle timeout (%s): %v", ErrUnavailable, r.limit, cause)
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.body.Close()
}
