package uc

import (
	"strings"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// sameHotel is the dedup predicate. Two records are the same hotel ONLY when
// their ISO country and city match AND their names match EXACTLY after trivial
// formatting is normalized away (lowercase, trimmed, internal whitespace
// collapsed). Word order and spelling must be identical — there is NO fuzzy/typo
// tolerance — so two genuinely different hotels that happen to have near-
// identical names in the same city are never wrongly merged. As a consequence,
// "Hotel Mare Azzurro" and "Mare Azzuro Hotel" (a typo + reorder) stay distinct.
func sameHotel(a, b ent.Hotel) bool {
	if a.Country != b.Country || !strings.EqualFold(a.City, b.City) {
		return false
	}
	return normalizeNameExact(a.Name) == normalizeNameExact(b.Name)
}

// normalizeNameExact lowercases, trims, and collapses internal whitespace. It
// deliberately preserves word order and spelling, so only trivial formatting
// differences between feeds are ignored — not spelling or ordering differences.
func normalizeNameExact(name string) string {
	return strings.TrimSpace(wsRun.ReplaceAllString(strings.ToLower(name), " "))
}
