package tui

import "testing"

func TestFuzzyScoreSubsequence(t *testing.T) {
	if _, ok := fuzzyScore("gen", "#general"); !ok {
		t.Fatal("expected 'gen' to match '#general'")
	}
	if _, ok := fuzzyScore("xyz", "#general"); ok {
		t.Fatal("expected 'xyz' not to match '#general'")
	}
}

func TestFuzzyScoreEmptyMatchesAll(t *testing.T) {
	if s, ok := fuzzyScore("", "anything"); !ok || s != 0 {
		t.Fatalf("empty pattern should match with score 0, got (%d,%v)", s, ok)
	}
}

func TestFuzzyScoreCaseInsensitive(t *testing.T) {
	if _, ok := fuzzyScore("GEN", "#general"); !ok {
		t.Fatal("match should be case-insensitive")
	}
}

func TestFuzzyScoreRanksTightOverLoose(t *testing.T) {
	tight, ok1 := fuzzyScore("abc", "abc")   // fully consecutive
	loose, ok2 := fuzzyScore("abc", "axbxc") // scattered, no extra word boundaries
	if !ok1 || !ok2 {
		t.Fatalf("both should match: tight=%v loose=%v", ok1, ok2)
	}
	if tight <= loose {
		t.Fatalf("tight consecutive match should outscore scattered: tight=%d loose=%d", tight, loose)
	}
}
