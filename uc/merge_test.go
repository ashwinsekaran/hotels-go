package uc

import (
	"testing"

	"github.com/ashwinsekaran/hotels-go/ent"
)

func TestSameHotelExactMatch(t *testing.T) {
	tests := []struct {
		name string
		a, b ent.Hotel
		want bool
	}{
		{
			"identical, trivial formatting differs",
			ent.Hotel{Name: "Hotel Mare Azzurro", City: "Rimini", Country: "IT"},
			ent.Hotel{Name: "hotel  mare   azzurro", City: "rimini", Country: "IT"},
			true,
		},
		{
			"typo keeps them distinct",
			ent.Hotel{Name: "Hotel Mare Azzurro", City: "Rimini", Country: "IT"},
			ent.Hotel{Name: "Mare Azzuro Hotel", City: "Rimini", Country: "IT"},
			false,
		},
		{
			"reordered words keep them distinct",
			ent.Hotel{Name: "Hotel Mare Azzurro", City: "Rimini", Country: "IT"},
			ent.Hotel{Name: "Mare Azzurro Hotel", City: "Rimini", Country: "IT"},
			false,
		},
		{
			"same name, different city",
			ent.Hotel{Name: "City Lodge", City: "Berlin", Country: "DE"},
			ent.Hotel{Name: "City Lodge", City: "Munich", Country: "DE"},
			false,
		},
		{
			"same name, different country",
			ent.Hotel{Name: "Grand Hotel", City: "Rimini", Country: "IT"},
			ent.Hotel{Name: "Grand Hotel", City: "Rimini", Country: "SM"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameHotel(tt.a, tt.b); got != tt.want {
				t.Fatalf("sameHotel = %v, want %v", got, tt.want)
			}
		})
	}
}

// Star reconciliation must be order-independent: the higher-trust source wins
// regardless of which record arrives first. Both records share an EXACT name so
// they dedup under the exact-match rule.
func TestMergeStarReconciliationOrderIndependent(t *testing.T) {
	partnerFirst := ingestPair(t,
		`{"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","stars":4}`,
		`{"source":"scrape-booking-sites","name":"Hotel Mare Azzurro","location":"Rimini, Italien","rating":"3 stars"}`,
	)
	scrapeFirst := ingestPair(t,
		`{"source":"scrape-booking-sites","name":"Hotel Mare Azzurro","location":"Rimini, Italien","rating":"3 stars"}`,
		`{"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","stars":4}`,
	)

	if partnerFirst.Stars == nil || *partnerFirst.Stars != 4 {
		t.Fatalf("partner-first stars = %v, want 4", partnerFirst.Stars)
	}
	if scrapeFirst.Stars == nil || *scrapeFirst.Stars != 4 {
		t.Fatalf("scrape-first stars = %v, want 4 (order-independent)", scrapeFirst.Stars)
	}
}

func TestMergeUnionsAmenitiesAndSources(t *testing.T) {
	h := ingestPair(t,
		`{"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","amenities":"pool,wifi"}`,
		`{"source":"scrape-booking-sites","name":"Hotel Mare Azzurro","location":"Rimini, Italien","features":["pets allowed","free WiFi"]}`,
	)
	for _, want := range []string{"pool", "wifi", "pets"} {
		if !contains(h.Amenities, want) {
			t.Fatalf("amenities %v missing %q", h.Amenities, want)
		}
	}
	if len(h.Sources) != 2 {
		t.Fatalf("sources not unioned: %v", h.Sources)
	}
}

// ingestPair ingests exactly two raw JSON objects and returns the single merged
// hotel, failing if they did not dedup into one.
func ingestPair(t *testing.T, a, b string) *ingestOne {
	t.Helper()
	store := newFakeStore()
	res := MakeIngestHotelsUc(store)(decodeFeed(t, "["+a+","+b+"]"))
	if len(res.Normalized) != 1 {
		t.Fatalf("expected 1 merged hotel, got %d (%+v)", len(res.Normalized), res.Errors)
	}
	return &ingestOne{res.Normalized[0].Stars, res.Normalized[0].Amenities, res.Normalized[0].Sources}
}

type ingestOne struct {
	Stars     *int
	Amenities []string
	Sources   []string
}
