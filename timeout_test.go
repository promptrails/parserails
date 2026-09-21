package parserails

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBoundedAbandonsSlowWork(t *testing.T) {
	p := &Parser{timeout: 50 * time.Millisecond}
	finished := make(chan struct{})

	_, err := bounded(context.Background(), p, func(context.Context) (int, error) {
		defer close(finished)
		time.Sleep(300 * time.Millisecond) // a PDFium call that will not stop
		return 7, nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}

	// The abandoned worker still runs to completion and releases itself;
	// nothing panics when it finally sends its result.
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("abandoned worker never finished")
	}
}

func TestBoundedReturnsFastResults(t *testing.T) {
	p := &Parser{timeout: time.Second}
	got, err := bounded(context.Background(), p, func(context.Context) (int, error) {
		return 42, nil
	})
	if err != nil || got != 42 {
		t.Fatalf("got (%v, %v), want (42, nil)", got, err)
	}
}

func TestBoundedHonoursCallerCancellation(t *testing.T) {
	p := &Parser{timeout: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := bounded(ctx, p, func(context.Context) (int, error) { return 1, nil }); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBoundedRunsInlineWithoutATimeout(t *testing.T) {
	p := &Parser{}
	got, err := bounded(context.Background(), p, func(ctx context.Context) (string, error) {
		if _, ok := ctx.Deadline(); ok {
			t.Error("no deadline should have been added")
		}
		return "inline", nil
	})
	if err != nil || got != "inline" {
		t.Fatalf("got (%q, %v)", got, err)
	}
}

func TestParseWithTimeoutStillParses(t *testing.T) {
	p := newTestParser(t, WithTimeout(30*time.Second))
	doc, err := p.Parse(context.Background(), minimalPDF("Hello"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := doc.Text(); got != "Hello" {
		t.Fatalf("text = %q", got)
	}
}

func TestAcquireTimeoutFollowsTheDocumentTimeout(t *testing.T) {
	if got := (&Parser{}).acquireTimeout(); got != defaultAcquireTimeout {
		t.Errorf("default = %s, want %s", got, defaultAcquireTimeout)
	}
	if got := (&Parser{timeout: time.Second}).acquireTimeout(); got != time.Second {
		t.Errorf("tight timeout = %s, want 1s", got)
	}
	if got := (&Parser{timeout: time.Hour}).acquireTimeout(); got != defaultAcquireTimeout {
		t.Errorf("loose timeout = %s, want the default", got)
	}
}
