package uc

import (
	"testing"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// Cross-field consistency (prose description vs structured amenities, e.g. an
// "adults-only" description alongside a "kids_club" amenity — as in the Sunset
// Bay record) was deliberately DEFERRED. This reserved, skipped test documents
// the intent and the expected shape so the slot exists when the rule table lands.
//
// TODO(cross-field-consistency): unskip and assert a warning is produced when a
// description contradicts the structured amenities.
func TestCrossFieldConsistency_Deferred(t *testing.T) {
	t.Skip("cross-field consistency: deferred by decision — see checkCrossFieldConsistency")

	adultsOnly := ent.Hotel{
		Name:        "Sunset Bay Resort",
		Description: "An adults-only sanctuary of tranquility.",
		Amenities:   []string{"kids_club", "pool", "spa"},
	}
	if got := checkCrossFieldConsistency(0, adultsOnly); len(got) == 0 {
		t.Fatalf("expected a cross-field contradiction warning once implemented")
	}
}
