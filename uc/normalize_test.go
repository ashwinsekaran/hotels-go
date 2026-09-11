package uc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ashwinsekaran/hotels-go/ent"
)

func TestParseStars(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      *int
		present   bool
		wantError bool
	}{
		{"int 4", `4`, intp(4), true, false},
		{"string '3 stars'", `"3 stars"`, intp(3), true, false},
		{"null", `null`, nil, false, false},
		{"absent", ``, nil, false, false},
		{"non-numeric string", `"deluxe"`, nil, true, true},
		{"zero out of range", `0`, nil, true, true},
		{"six out of range", `6`, nil, true, true},
		{"negative", `-1`, nil, true, true},
		{"decimal rejected", `4.5`, nil, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, present, err := parseStars(json.RawMessage(tt.raw))
			if present != tt.present {
				t.Fatalf("present = %v, want %v", present, tt.present)
			}
			if (err != nil) != tt.wantError {
				t.Fatalf("err = %v, wantError = %v", err, tt.wantError)
			}
			if !intPtrEqual(got, tt.want) {
				t.Fatalf("stars = %v, want %v", deref(got), deref(tt.want))
			}
		})
	}
}

func TestParsePrice(t *testing.T) {
	tests := []struct {
		name      string
		raw       ent.RawHotel
		present   bool
		wantError bool
		wantCents int
		wantCur   string
	}{
		{"eur number", ent.RawHotel{PriceFromEUR: json.RawMessage(`89`)}, true, false, 8900, "EUR"},
		{"usd string", ent.RawHotel{PriceFrom: json.RawMessage(`"180 USD"`)}, true, false, 18000, "USD"},
		{"absent", ent.RawHotel{}, false, false, 0, ""},
		{"unknown currency", ent.RawHotel{PriceFrom: json.RawMessage(`"180 XYZ"`)}, true, true, 0, ""},
		{"negative eur", ent.RawHotel{PriceFromEUR: json.RawMessage(`-5`)}, true, true, 0, ""},
		{"garbage string", ent.RawHotel{PriceFrom: json.RawMessage(`"cheap!"`)}, true, true, 0, ""},
		{"absurdly large", ent.RawHotel{PriceFromEUR: json.RawMessage(`99999999`)}, true, true, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, present, err := parsePrice(tt.raw)
			if present != tt.present {
				t.Fatalf("present = %v, want %v", present, tt.present)
			}
			if (err != nil) != tt.wantError {
				t.Fatalf("err = %v, wantError = %v", err, tt.wantError)
			}
			if err == nil && got != nil {
				if got.AmountCents != tt.wantCents || got.Currency != tt.wantCur {
					t.Fatalf("money = %+v, want {%d %s}", *got, tt.wantCents, tt.wantCur)
				}
			}
		})
	}
}

func TestParseAmenities(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      []string
		wantError bool
	}{
		{"comma string", `"pool,wifi,parking"`, []string{"parking", "pool", "wifi"}, false},
		{"array with synonyms", `["swimming pool","free WiFi","pets allowed"]`, []string{"pets", "pool", "wifi"}, false},
		{"null", `null`, []string{}, false},
		{"absent", ``, []string{}, false},
		{"quirky 3 pools", `["3 pools","spa"]`, []string{"pool", "spa"}, false},
		{"blanks and dupes", `"wifi, wifi, ,parking"`, []string{"parking", "wifi"}, false},
		{"unknown slugified", `["swim-up bar"]`, []string{"swim_up_bar"}, false},
		{"invalid type", `123`, []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmenities(json.RawMessage(tt.raw))
			if (err != nil) != tt.wantError {
				t.Fatalf("err = %v, wantError = %v", err, tt.wantError)
			}
			if !stringSlicesEqual(got, tt.want) {
				t.Fatalf("amenities = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeCountry(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"IT", "IT", true},
		{"Italien", "IT", true}, // German spelling in the scrape feed
		{"italy", "IT", true},
		{"Mexico", "MX", true},
		{"Deutschland", "DE", true},
		{"Atlantis", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := normalizeCountry(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("normalizeCountry(%q) = %q,%v want %q,%v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSplitLocation(t *testing.T) {
	tests := []struct {
		in      string
		city    string
		country string
		ok      bool
	}{
		{"Rimini, Italien", "Rimini", "IT", true},
		{"Playa del Carmen, Mexico", "Playa del Carmen", "MX", true},
		{"Rimini", "", "", false},           // no comma
		{"Rimini, Atlantis", "", "", false}, // unresolvable country
		{"", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			city, country, ok := splitLocation(tt.in)
			if city != tt.city || country != tt.country || ok != tt.ok {
				t.Fatalf("splitLocation(%q) = %q,%q,%v want %q,%q,%v",
					tt.in, city, country, ok, tt.city, tt.country, tt.ok)
			}
		})
	}
}

func TestStripHTMLAndSanitize(t *testing.T) {
	desc, _ := sanitizeText(ptr(stripHTML("<b>WELCOME TO PARADISE!!!</b> Newly renovated")), maxDescLen)
	if strings.Contains(desc, "<") || strings.Contains(desc, ">") {
		t.Fatalf("HTML not stripped: %q", desc)
	}
	if !strings.Contains(desc, "WELCOME TO PARADISE") {
		t.Fatalf("text lost: %q", desc)
	}

	// Control characters removed, valid text kept.
	clean, _ := sanitizeText(ptr("hello\x00\x07world\n"), maxDescLen)
	if clean != "helloworld" {
		t.Fatalf("control chars not handled: %q", clean)
	}

	// Truncation flag.
	long := strings.Repeat("a", maxDescLen+10)
	_, truncated := sanitizeText(&long, maxDescLen)
	if !truncated {
		t.Fatalf("expected truncation for over-length input")
	}
}

func TestHotelIDStable(t *testing.T) {
	a := hotelID("Hotel Mare Azzurro", "Rimini", "IT")
	b := hotelID("  hotel mare azzurro ", "rimini", "IT") // case/space variants
	if a != b {
		t.Fatalf("hotelID not stable across case/space: %q vs %q", a, b)
	}
	c := hotelID("Alpenhof Garni", "Innsbruck", "AT")
	if a == c {
		t.Fatalf("different hotels collided on id")
	}
}

func TestValidateCoords(t *testing.T) {
	// In-range, near the Rimini centroid -> accepted, no warning.
	pt, cerr, cwarn := validateCoords(ent.GeoPoint{Lat: 44.06, Lng: 12.57}, "Rimini", "IT")
	if pt == nil || cerr != "" || cwarn != "" {
		t.Fatalf("plausible coords rejected/flagged: pt=%v cerr=%q cwarn=%q", pt, cerr, cwarn)
	}

	// City Lodge Berlin: coords actually point at Munich -> kept but flagged.
	pt, cerr, cwarn = validateCoords(ent.GeoPoint{Lat: 48.1351, Lng: 11.5820}, "Berlin", "DE")
	if pt == nil || cerr != "" {
		t.Fatalf("plausibility should warn, not drop: cerr=%q", cerr)
	}
	if cwarn == "" {
		t.Fatalf("expected implausibility warning for Berlin coords in Munich")
	}

	// Out of range -> dropped.
	pt, cerr, _ = validateCoords(ent.GeoPoint{Lat: 999, Lng: 0}, "Rimini", "IT")
	if pt != nil || cerr == "" {
		t.Fatalf("out-of-range coords should be dropped with an error")
	}
}

// ---- helpers ----

func intp(n int) *int { return &n }

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
