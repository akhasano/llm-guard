package detectors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// TermsDetector finds configured literal substrings, ignoring Unicode case.
// It deliberately does not impose word boundaries or interpret regex syntax.
type TermsDetector struct {
	patterns []*regexp.Regexp
}

// NewTermsDetector rejects empty entries without including sensitive values
// in errors. Whitespace in non-empty terms is preserved exactly.
func NewTermsDetector(terms []string) (*TermsDetector, error) {
	d := &TermsDetector{}
	for i, term := range terms {
		if strings.TrimSpace(term) == "" {
			return nil, fmt.Errorf("term at index %d must not be empty or whitespace-only", i)
		}
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(term))
		if err != nil {
			return nil, fmt.Errorf("invalid term at index %d", i)
		}
		d.patterns = append(d.patterns, re)
	}
	return d, nil
}

func (d *TermsDetector) Name() string { return "terms" }

func (d *TermsDetector) Detect(text string) []Match {
	var matches []Match
	for _, re := range d.patterns {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			matches = append(matches, Match{
				Category: "custom_term",
				Value:    text[loc[0]:loc[1]],
				Start:    loc[0],
				End:      loc[1],
			})
		}
	}
	// Merge overlaps so a shorter match cannot leave part of another term visible.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Start < matches[j].Start
	})
	merged := matches[:0]
	for _, m := range matches {
		if len(merged) > 0 && m.Start < merged[len(merged)-1].End {
			last := &merged[len(merged)-1]
			if m.End > last.End {
				last.End = m.End
				last.Value = text[last.Start:last.End]
			}
			continue
		}
		merged = append(merged, m)
	}
	return merged
}
