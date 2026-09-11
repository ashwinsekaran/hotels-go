// Package ent holds the domain entities: plain data structs with json tags and
// no third-party dependencies. It is the innermost layer — nothing here imports
// any other package in this service. Every other layer may depend on ent; ent
// depends on nothing but the standard library.
//
// Two kinds of struct live here:
//   - Hotel — the CANONICAL model every provider is normalized into.
//   - RawHotel — the permissive INBOUND shape that can absorb any provider's
//     JSON without a decode error (shape-shifting fields are json.RawMessage).
//
// It also holds the value objects (Money, GeoPoint) and the structured result
// types the use-case layer fills in (RecordError = fatal, RecordWarning = soft).
package ent

import "encoding/json"

// Hotel is the canonical, normalized entity. Optional fields are pointers so
// "absent" (nil) is distinguishable from a real zero value. Amenities and
// Sources are always non-nil (possibly empty) slices for stable JSON output.
type Hotel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	City        string    `json:"city"`
	Country     string    `json:"country"` // ISO-3166 alpha-2 (IT, AT, DE, MX)
	Stars       *int      `json:"stars"`   // official classification 1–5, or null
	Description string    `json:"description"`
	Amenities   []string  `json:"amenities"`             // canonical vocabulary, deduped, sorted
	Price       *Money    `json:"price,omitempty"`       // native currency; nil if unknown
	Coordinates *GeoPoint `json:"coordinates,omitempty"` // nil if the provider sent none
	Sources     []string  `json:"sources"`               // provenance / audit trail
}

// Money represents a price in integer minor units (cents) plus its ISO-4217
// currency code. Integer cents avoid floating-point rounding. The value is
// always stored in the currency the provider supplied — never converted.
type Money struct {
	AmountCents int    `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// GeoPoint is a WGS-84 latitude/longitude pair. Reused to decode the providers'
// nested "coords" object.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// RawHotel is the tolerant inbound DTO. Every field a provider MIGHT send has a
// slot; scalars that are genuinely optional are pointers (nil = absent), and the
// fields whose JSON TYPE varies across providers (stars/rating, amenities/
// features, the two price shapes) are json.RawMessage so decoding never fails on
// a type mismatch — the use-case layer interprets them per source.
type RawHotel struct {
	Source string `json:"source"`

	// Name: partner feeds use hotel_name; the scrape uses name.
	HotelName *string `json:"hotel_name"`
	Name      *string `json:"name"`

	// Location: partner feeds split city+country; the scrape ships one string.
	City     *string `json:"city"`
	Country  *string `json:"country"`
	Location *string `json:"location"`

	// Rating: partner feeds send an int stars; the scrape sends "3 stars".
	Stars  json.RawMessage `json:"stars"`
	Rating json.RawMessage `json:"rating"`

	Description *string `json:"description"`

	// Amenities: partner-feed-a sends a comma string (or null); the scrape sends
	// an array under "features".
	Amenities json.RawMessage `json:"amenities"`
	Features  json.RawMessage `json:"features"`

	// Price: partner-feed-a sends an EUR number; the scrape sends "180 USD".
	PriceFromEUR json.RawMessage `json:"price_from_eur"`
	PriceFrom    json.RawMessage `json:"price_from"`

	Coords         *GeoPoint `json:"coords"`
	ReviewSnippets []string  `json:"review_snippets"`
	LastSeen       *string   `json:"last_seen"`
}

// RecordError is a FATAL, per-record problem: the record could not be normalized
// and was dropped from the result (e.g. unknown source, missing name/location).
// It carries enough context to point the caller at the exact offending input.
type RecordError struct {
	Index   int    `json:"index"`  // position in the incoming batch
	Source  string `json:"source"` // provider source, if known
	Field   string `json:"field"`  // offending field
	Message string `json:"message"`
}

// RecordWarning is a SOFT, non-fatal signal: the record was kept, but something
// was repaired, reconciled, or flagged for a human (e.g. an out-of-range star
// value dropped to null, a cross-source star conflict, implausible coordinates,
// or a parked cross-field-consistency TODO). Warnings never block ingestion.
type RecordWarning struct {
	Index   int    `json:"index"`              // incoming position, or -1 for merge-level
	HotelID string `json:"hotel_id,omitempty"` // canonical id once known
	Field   string `json:"field"`
	Code    string `json:"code"` // machine-readable, e.g. "stars_conflict"
	Message string `json:"message"`
}
