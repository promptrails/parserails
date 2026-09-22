package parserails

import (
	"context"
	"os"
	"path/filepath"
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

func TestBoundedEnforcesATighterCallerDeadline(t *testing.T) {
	// The caller's deadline is shorter than the parser's timeout. The engine
	// cannot see either, so the wait must still be abandoned at the earlier
	// of the two rather than run to completion.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := bounded(ctx, &Parser{timeout: 5 * time.Second}, func(context.Context) (int, error) {
		time.Sleep(3 * time.Second)
		return 1, nil
	})
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("waited %s for a 50ms deadline", elapsed)
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestBoundedEnforcesACallerDeadlineWithNoParserTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := bounded(ctx, &Parser{}, func(context.Context) (int, error) {
		time.Sleep(3 * time.Second)
		return 1, nil
	})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waited %s for a 50ms deadline", elapsed)
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestOfficeConversionRespectsTheTimeout(t *testing.T) {
	// A "LibreOffice" that hangs. Conversion must be abandoned with the rest
	// of the document work, not waited on forever.
	dir := t.TempDir()
	fake := filepath.Join(dir, "soffice")
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil { // #nosec G306
		t.Fatal(err)
	}
	p := newTestParser(t, WithLibreOffice(fake), WithTimeout(300*time.Millisecond))

	docx := zipArchive(map[string][]byte{
		"word/document.xml": []byte(`<w:document xmlns:w="x"><w:body/></w:document>`),
	})
	start := time.Now()
	_, err := p.ParseData(context.Background(), docx, ReadOptions{Name: "stuck.docx"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected the conversion to be cut short")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("waited %s for a 300ms timeout: %v", elapsed, err)
	}
}

func TestAwaitBoundedPrefersAFinishedOperation(t *testing.T) {
	// The operation finished in the same instant the deadline passed. select
	// picks at random between two ready cases, so this is run many times: a
	// result that exists must never be thrown away for a timeout.
	for i := 0; i < 2000; i++ {
		done := make(chan boundedResult[int], 1)
		done <- boundedResult[int]{value: 42}

		inner, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := awaitBounded(context.Background(), inner, done, time.Second)
		if err != nil || got != 42 {
			t.Fatalf("iteration %d: got (%v, %v), want (42, nil)", i, got, err)
		}
	}
}

func TestAwaitBoundedReportsATimeoutWhenNothingFinished(t *testing.T) {
	done := make(chan boundedResult[int], 1)
	inner, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := awaitBounded(context.Background(), inner, done, 250*time.Millisecond); err == nil ||
		!strings.Contains(err.Error(), "timed out after 250ms") {
		t.Fatalf("err = %v, want a timeout", err)
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
