package parserails

import (
	"context"
	"fmt"
	"time"
)

// defaultAcquireTimeout is how long a call waits for a free PDFium instance
// before giving up.
const defaultAcquireTimeout = 30 * time.Second

// bounded runs one document operation under the earlier of the parser's
// timeout and the caller's own deadline.
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

	callerDeadline, hasDeadline := ctx.Deadline()
	if p.timeout <= 0 && !hasDeadline {
		return op(ctx) // nothing to enforce: run inline, no goroutine
	}

	inner, cancel := ctx, context.CancelFunc(func() {})
	if p.timeout > 0 && (!hasDeadline || time.Until(callerDeadline) > p.timeout) {
		inner, cancel = context.WithTimeout(ctx, p.timeout)
	}

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
		// A caller's own deadline is enforced here too. Letting it "govern"
		// by running inline would mean nothing enforced it at all: the engine
		// never looks at the context once a call is under way.
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return zero, fmt.Errorf("parserails: document timed out after %s", p.timeout)
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
