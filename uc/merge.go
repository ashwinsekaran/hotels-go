package uc

import (
	"fmt"
	"sort"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// mergeHotels reconciles an incoming record into an already-stored hotel they
// were judged to be the same (see sameHotel). It keeps the existing id, unions
// provenance and amenities, and resolves conflicting scalar fields by TRUST TIER
// (higher-tier source wins; ties keep the existing value). Every genuine conflict
// produces a warning so the disagreement is surfaced, never silently discarded.
//
// The result is order-independent in content: whichever record arrives first,
// the higher-tier source's values win.
func mergeHotels(existing, incoming ent.Hotel, index int) (ent.Hotel, []ent.RecordWarning) {
	var warns []ent.RecordWarning
	warn := func(field, code, msg string) {
		warns = append(warns, ent.RecordWarning{
			Index: index, HotelID: existing.ID, Field: field, Code: code, Message: msg,
		})
	}

	out := existing
	exTier := maxTier(existing.Sources)
	inTier := maxTier(incoming.Sources)
	incomingWins := inTier > exTier

	out.Sources = unionStrings(existing.Sources, incoming.Sources)
	out.Amenities = unionSorted(existing.Amenities, incoming.Amenities)

	// Stars.
	switch {
	case out.Stars == nil:
		out.Stars = incoming.Stars
	case incoming.Stars != nil && *incoming.Stars != *out.Stars:
		msg := fmt.Sprintf("stars conflict: %d (%v) vs %d (%v)",
			*out.Stars, existing.Sources, *incoming.Stars, incoming.Sources)
		if incomingWins {
			out.Stars = incoming.Stars
		}
		warn("stars", "stars_conflict", msg)
	}

	// Price.
	switch {
	case out.Price == nil:
		out.Price = incoming.Price
	case incoming.Price != nil && *incoming.Price != *out.Price:
		msg := fmt.Sprintf("price conflict: %d %s vs %d %s",
			out.Price.AmountCents, out.Price.Currency,
			incoming.Price.AmountCents, incoming.Price.Currency)
		if incomingWins {
			out.Price = incoming.Price
		}
		warn("price", "price_conflict", msg)
	}

	// Coordinates: fill if missing; if both present and they differ notably, keep
	// the existing one but flag it.
	switch {
	case out.Coordinates == nil:
		out.Coordinates = incoming.Coordinates
	case incoming.Coordinates != nil && haversineKm(*out.Coordinates, *incoming.Coordinates) > coordPlausibilityKm:
		if incomingWins {
			out.Coordinates = incoming.Coordinates
		}
		warn("coordinates", "coordinates_conflict", "sources disagree on coordinates")
	}

	// Description: prefer a non-empty value; on conflict the higher tier wins.
	if out.Description == "" {
		out.Description = incoming.Description
	} else if incoming.Description != "" && incomingWins {
		out.Description = incoming.Description
	}

	return out, warns
}

// maxTier returns the highest trust tier among a hotel's sources.
func maxTier(sources []string) int {
	best := 0
	for _, s := range sources {
		if t, ok := sourceTier(s); ok && t > best {
			best = t
		}
	}
	return best
}

// unionStrings appends items from b that are not already in a, preserving order.
func unionStrings(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// unionSorted unions two sets and returns them sorted (amenities are order-free).
func unionSorted(a, b []string) []string {
	out := unionStrings(a, b)
	sort.Strings(out)
	return out
}
