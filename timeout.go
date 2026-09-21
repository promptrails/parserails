package parserails

import (
	"context"
	"fmt"
	"time"
)

// defaultAcquireTimeout is how long a call waits for a free PDFium instance
// before giving up.
const defaultAcquireTimeout = 30 * time.Second

// bounded runs one document operation under the parser's timeout.
//
// PDFium calls cannot be interrupted: neither the WebAssembly runtime nor the
// native library polls a cancellation signal, so a pathological document can
// occupy a worker for as long as it likes. The operation therefore runs on its
// own goroutine and the caller stops waiting when the deadline passes. The
// abandoned worker still finishes and still releases its instance — it is a
// worker that is busy for too long, not a leak — so the pipeline keeps moving
// while one bad document finishes dying.
func bounded[T any](ctx context.Context, p *Parser, op func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	timeout := p.timeout
	if timeout <= 0 {
		return op(ctx) // no deadline configured: run inline, no goroutine
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return op(ctx) // the caller's own deadline is tighter; let it govern
	}

	inner, cancel := context.WithTimeout(ctx, timeout)
	type result struct {
		value T
		err   error
	}
	// Buffered: the abandoned goroutine must never block on a send nobody is
	// waiting for.
	done := make(chan result, 1)
	go func() {
		defer cancel()
		value, err := op(inner)
		done <- result{value, err}
	}()

	select {
	case r := <-done:
		return r.value, r.err
	case <-inner.Done():
		if ctx.Err() != nil {
			return zero, ctx.Err() // the caller cancelled
		}
		return zero, fmt.Errorf("parserails: document timed out after %s", timeout)
	}
}

// acquireTimeout is how long to wait for a worker: the parser's timeout when
// it is shorter than the default, so a tight deadline is not spent queueing.
func (p *Parser) acquireTimeout() time.Duration {
	if p.timeout > 0 && p.timeout < defaultAcquireTimeout {
		return p.timeout
	}
	return defaultAcquireTimeout
}
