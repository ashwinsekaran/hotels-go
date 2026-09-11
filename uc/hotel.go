// Package uc is the use-case (application) layer. It OWNS the repository port as
// an interface and depends only on that interface plus the innermost ent layer
// (Dependency Inversion). It must NEVER import repo, handlers, metrics, auth,
// health, or main.
//
// This layer is the single source of truth for what makes a valid hotel. It runs
// three sequential phases on ingest:
//
//  1. parse & coerce  — raw provider fields → typed canonical values (normalize.go)
//  2. invariants      — validate the assembled Hotel (normalize.go)
//  3. merge/reconcile — dedup against existing hotels and reconcile conflicts (merge.go)
//
// Use-cases are function types built by factory funcs that close over a store, so
// each is a single value that is trivial to build and to unit-test with a fake
// store — no HTTP and no database required.
package uc

import (
	"sort"
	"strings"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// HotelStore is the port the use-case layer defines and depends on. The repo
// layer supplies a concrete implementation; swapping the in-memory repo for a
// database means writing a new type that satisfies THIS interface — with no
// change to this file or to handlers/main.
//
// All() is required because dedup matching is BUSINESS logic that lives here: the
// ingest use-case scans existing hotels to find a fuzzy match. Lookups use Go's
// comma-ok idiom for "not found" rather than sentinel errors.
type HotelStore interface {
	GetById(id string) (ent.Hotel, bool)
	Save(h ent.Hotel)
	All() []ent.Hotel
}

// IngestResult is what a POST returns. Normalized holds the deduped canonical
// hotels touched by this batch; Errors are fatal per-record failures (record
// dropped); Warnings are soft signals (record kept but repaired/reconciled/
// flagged). All three are always non-nil so the JSON output is stable.
type IngestResult struct {
	Normalized []ent.Hotel         `json:"normalized"`
	Errors     []ent.RecordError   `json:"errors"`
	Warnings   []ent.RecordWarning `json:"warnings"`
}

// HotelFilter is the query for a list. Zero values mean "no filter on this
// field"; MinStars is a pointer so a caller can filter on >= without a filter
// being implied by the zero int.
type HotelFilter struct {
	City     string
	Country  string
	MinStars *int
}

// Use-case function types — one per operation, prefixed by the resource.
type (
	IngestHotelsUc func(raws []ent.RawHotel) IngestResult
	ListHotelsUc   func(f HotelFilter) []ent.Hotel
	GetHotelUc     func(id string) (ent.Hotel, bool)
)

// MakeGetHotelUc injects the store into the returned closure.
func MakeGetHotelUc(store HotelStore) GetHotelUc {
	return func(id string) (ent.Hotel, bool) {
		return store.GetById(id)
	}
}

// MakeListHotelsUc returns a use-case that lists hotels matching the filter.
// Filtering is business logic and lives here (not in repo), so a DB adapter
// stays a dumb store. Output is sorted by ID for deterministic responses.
func MakeListHotelsUc(store HotelStore) ListHotelsUc {
	return func(f HotelFilter) []ent.Hotel {
		out := make([]ent.Hotel, 0)
		for _, h := range store.All() {
			if f.City != "" && !strings.EqualFold(h.City, f.City) {
				continue
			}
			if f.Country != "" && !strings.EqualFold(h.Country, f.Country) {
				continue
			}
			if f.MinStars != nil && (h.Stars == nil || *h.Stars < *f.MinStars) {
				continue
			}
			out = append(out, h)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
}

// MakeIngestHotelsUc returns the ingest use-case: for each raw record it
// normalizes (parse + invariants), and on success dedups against existing hotels
// (fuzzy match on city+country+name) — merging into the matched hotel or storing
// a new one. Fatal problems become Errors and the record is skipped; the batch is
// NEVER aborted on the first bad record (errors accumulate).
func MakeIngestHotelsUc(store HotelStore) IngestHotelsUc {
	return func(raws []ent.RawHotel) IngestResult {
		res := IngestResult{
			Normalized: make([]ent.Hotel, 0, len(raws)),
			Errors:     make([]ent.RecordError, 0),
			Warnings:   make([]ent.RecordWarning, 0),
		}

		// touched preserves first-seen order of the canonical hotels this batch
		// created or updated, so the response is stable and deduped.
		var touched []string
		seen := map[string]bool{}

		for i, raw := range raws {
			hotel, errs, warns := normalizeRecord(i, raw)
			if len(errs) > 0 {
				res.Errors = append(res.Errors, errs...)
				continue // fatal — record dropped
			}
			res.Warnings = append(res.Warnings, warns...)

			// PARKED TODO: cross-field consistency (description vs amenities,
			// e.g. "adults-only" + "kids club"). Deferred by decision; the stub
			// documents where it will hook in and returns no warnings today.
			res.Warnings = append(res.Warnings, checkCrossFieldConsistency(i, hotel)...)

			if match, ok := findMatch(store, hotel); ok {
				merged, mwarns := mergeHotels(match, hotel, i)
				res.Warnings = append(res.Warnings, mwarns...)
				store.Save(merged)
				hotel = merged
			} else {
				store.Save(hotel)
			}

			if !seen[hotel.ID] {
				seen[hotel.ID] = true
				touched = append(touched, hotel.ID)
			}
		}

		// Reload the touched hotels from the store so the response reflects the
		// final merged state (a later record in the batch may have updated an
		// earlier one).
		for _, id := range touched {
			if h, ok := store.GetById(id); ok {
				res.Normalized = append(res.Normalized, h)
			}
		}
		return res
	}
}

// findMatch scans existing hotels for one that is the same hotel as incoming,
// using the fuzzy, city+country-scoped rule in similarity.go. This is why the
// port exposes All(): matching is business logic, not a storage concern.
func findMatch(store HotelStore, incoming ent.Hotel) (ent.Hotel, bool) {
	for _, existing := range store.All() {
		if sameHotel(existing, incoming) {
			return existing, true
		}
	}
	return ent.Hotel{}, false
}

// checkCrossFieldConsistency is the parked TODO hook. Cross-field semantic
// contradiction detection (prose description vs structured amenities) was
// deliberately deferred — it needs a curated rule table or an LLM, the latter of
// which reintroduces prompt-injection risk on untrusted text. For now it is a
// no-op so the wiring and the (skipped) test slot exist.
//
// TODO(cross-field-consistency): flag contradictions such as an "adults-only"
// description alongside a "kids_club" amenity, as RecordWarnings (never errors).
func checkCrossFieldConsistency(_ int, _ ent.Hotel) []ent.RecordWarning {
	return nil
}
