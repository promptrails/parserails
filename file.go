package parserails

import (
	"context"
	"sync"
)

// FileResult pairs a parsed document with its source path and any error.
type FileResult struct {
	Path     string
	Document *Document
	Err      error
}

// ParseFiles parses many files concurrently, running up to concurrency parses at
// once (concurrency <= 0 means len(paths)). Results are returned in the same
// order as paths; a per-file error is captured in FileResult.Err rather than
// aborting the batch.
func (p *Parser) ParseFiles(ctx context.Context, paths []string, concurrency int) []FileResult {
	results := make([]FileResult, len(paths))
	if concurrency <= 0 || concurrency > len(paths) {
		concurrency = len(paths)
	}
	if concurrency == 0 {
		return results
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			doc, err := p.ParseFile(ctx, path)
			results[i] = FileResult{Path: path, Document: doc, Err: err}
		}(i, path)
	}
	wg.Wait()
	return results
}
