package uc

import (
	"sort"
	"strings"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// nameLevThreshold is the maximum Levenshtein distance between two normalized
// hotel names (already stopword-stripped and token-sorted) for them to be
// considered the same hotel. 2 tolerates a transposition plus a typo — e.g.
// "azzurro mare" vs "azzuro mare" (distance 1) — while staying tight enough that
// genuinely different names in the same city do not collide.
const nameLevThreshold = 2

// nameStopwords are dropped before comparison: they carry no identifying signal
// and differ freely across feeds ("Hotel Mare Azzurro" vs "Mare Azzuro Hotel").
var nameStopwords = map[string]bool{
	"hotel": true, "hotels": true, "resort": true, "resorts": true,
	"spa": true, "the": true, "and": true, "&": true,
	"garni": true, "inn": true, "suites": true, "suite": true,
	"guesthouse": true, "motel": true,
}

// sameHotel is the dedup predicate: the canonical city and ISO country must match
// exactly (scoping the fuzzy step), then names are compared fuzzily. City+country
// scoping is what keeps the Levenshtein match from producing false merges across
// unrelated hotels.
func sameHotel(a, b ent.Hotel) bool {
	if a.Country != b.Country || !strings.EqualFold(a.City, b.City) {
		return false
	}
	return namesSimilar(a.Name, b.Name)
}

// namesSimilar reports whether two hotel names refer to the same hotel after
// normalization (lowercase, punctuation stripped, stopwords removed, tokens
// sorted) and a Levenshtein-distance check.
func namesSimilar(a, b string) bool {
	na := normalizeName(a)
	nb := normalizeName(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	return levenshtein(na, nb) <= nameLevThreshold
}

// normalizeName lowercases, strips non-alphanumeric characters, removes stopwords
// and sorts the remaining tokens so word order does not matter.
func normalizeName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	tokens := strings.Fields(b.String())
	kept := tokens[:0]
	for _, t := range tokens {
		if !nameStopwords[t] {
			kept = append(kept, t)
		}
	}
	sort.Strings(kept)
	return strings.Join(kept, " ")
}

// levenshtein computes the edit distance between two strings (classic two-row
// dynamic programming, O(len(a)*len(b)) time, O(len(b)) space).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
