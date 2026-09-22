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
	// Cancelled by whoever stops waiting, not by the worker: on the timeout
	// path this also tells an abandoned operation that can hear it — a
	// LibreOffice subprocess — to stop.
	defer cancel()

	// Buffered: the abandoned goroutine must never block on a send nobody is
	// waiting for.
	done := make(chan boundedResult[T], 1)
	go func() {
		value, err := op(inner)
		done <- boundedResult[T]{value, err}
	}()

	return awaitBounded(ctx, inner, done, p.timeout)
}

// boundedResult carries an operation's outcome off its goroutine.
type boundedResult[T any] struct {
	value T
	err   error
}

// awaitBounded waits for the operation or for the deadline, whichever comes
// first — preferring the operation when both are ready.
//
// That preference is the point: select chooses uniformly at random among
// ready cases, so an operation that finishes in the same instant the deadline
// passes was reported as a timeout about half the time it raced, discarding a
// result that had already been computed.
func awaitBounded[T any](ctx, inner context.Context, done <-chan boundedResult[T], timeout time.Duration) (T, error) {
	var zero T
	select {
	case r := <-done:
		return r.value, r.err
	case <-inner.Done():
		select {
		case r := <-done:
			return r.value, r.err
		default:
		}
		if err := ctx.Err(); err != nil {
			return zero, err
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
