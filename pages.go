package parserails

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePageSpec expands a 1-based page selection like "1-5,10,15-20" into
// 0-based page indexes, in the order written, without duplicates.
//
// Out-of-range pages are skipped rather than rejected: a pipeline that asks for
// "1-10" of a 3-page document wants those three pages, not an error.
func parsePageSpec(spec string, count int) ([]int, error) {
	var (
		out  []int
		seen = make(map[int]bool)
	)
	add := func(i int) {
		if i >= 0 && i < count && !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		from, to, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(from))
		if err != nil {
			return nil, fmt.Errorf("parserails: bad page spec %q", part)
		}
		end := start
		if isRange {
			if end, err = strconv.Atoi(strings.TrimSpace(to)); err != nil {
				return nil, fmt.Errorf("parserails: bad page spec %q", part)
			}
		}
		if start < 1 || end < start {
			return nil, fmt.Errorf("parserails: bad page range %q (pages are 1-based)", part)
		}
		for i := start; i <= end; i++ {
			add(i - 1)
		}
	}
	return out, nil
}

// selectPages resolves which pages of a count-page document to read.
func selectPages(count int, opt ReadOptions) ([]int, error) {
	pages := make([]int, 0, count)
	if spec := strings.TrimSpace(opt.Pages); spec != "" {
		selected, err := parsePageSpec(spec, count)
		if err != nil {
			return nil, err
		}
		pages = selected
	} else {
		for i := 0; i < count; i++ {
			pages = append(pages, i)
		}
	}
	if opt.MaxPages > 0 && len(pages) > opt.MaxPages {
		pages = pages[:opt.MaxPages]
	}
	return pages, nil
}
