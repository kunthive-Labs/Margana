package tui

import (
	"strings"
	"unicode"
)

// fuzzyScore reports whether pattern is a subsequence of text
// (case-insensitive) and, if so, a relevance score — higher is better. An empty
// pattern matches everything with score 0 so callers keep the natural ordering.
//
// Scoring rewards consecutive matches and matches at the start of a word or
// segment (the char after a non-alphanumeric like '#', '/', '-'), and gently
// prefers shorter targets. It is a small, pure-Go matcher in the spirit of the
// hand-rolled highlighter in markdown.go — no external dependency.
func fuzzyScore(pattern, text string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	p := []rune(strings.ToLower(pattern))
	lower := []rune(strings.ToLower(text))
	orig := []rune(text)

	score := 0
	pi := 0
	prev := -2
	for ti := 0; ti < len(lower) && pi < len(p); ti++ {
		if lower[ti] != p[pi] {
			continue
		}
		s := 1
		if ti == prev+1 {
			s += 3 // consecutive characters
		}
		if ti == 0 || !isAlnum(orig[ti-1]) {
			s += 5 // start of a word/segment
		}
		score += s
		prev = ti
		pi++
	}
	if pi != len(p) {
		return 0, false // not a subsequence
	}
	score -= len(lower) / 10 // gently prefer tighter matches
	return score, true
}

func isAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
