package uc

import "testing"

func TestNamesSimilar(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"Hotel Mare Azzurro", "Mare Azzuro Hotel", true}, // word order + typo
		{"Alpenhof Garni", "Alpenhof", true},              // stopword dropped
		{"City Lodge Berlin", "Beach Resort Cancun", false},
		{"Sunset Bay Resort & Spa", "Sunset Bay", true},
	}
	for _, tt := range tests {
		if got := namesSimilar(tt.a, tt.b); got != tt.want {
			t.Fatalf("namesSimilar(%q,%q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"azzurro mare", "azzuro mare", 1},
		{"", "abc", 3},
		{"same", "same", 0},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Fatalf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Star reconciliation must be order-independent: the higher-trust source wins
// regardless of which record arrives first.
func TestMergeStarReconciliationOrderIndependent(t *testing.T) {
	partnerFirst := ingestPair(t,
		`{"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","stars":4}`,
		`{"source":"scrape-booking-sites","name":"Mare Azzuro Hotel","location":"Rimini, Italien","rating":"3 stars"}`,
	)
	scrapeFirst := ingestPair(t,
		`{"source":"scrape-booking-sites","name":"Mare Azzuro Hotel","location":"Rimini, Italien","rating":"3 stars"}`,
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
		`{"source":"scrape-booking-sites","name":"Mare Azzuro Hotel","location":"Rimini, Italien","features":["pets allowed","free WiFi"]}`,
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
