package uc

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// ---- Limits (abuse / resource bounds; applied on every record) ----
const (
	maxNameLen    = 200
	maxCityLen    = 120
	maxDescLen    = 5000
	maxAmenities  = 100
	maxAmenityLen = 60
	maxPriceCents = 100_000_00 // 100,000 of any currency/night — above this is almost certainly a parse error
)

// knownSources is the provider registry. It validates the inbound source (an
// unknown source is a fatal error) AND carries a trust tier used to reconcile
// cross-source conflicts: higher tier wins. Verified partner feeds outrank the
// noisier scraper.
var knownSources = map[string]int{
	"partner-feed-a":       2,
	"partner-feed-b":       2,
	"scrape-booking-sites": 1,
}

// sourceTier reports the trust tier for a source and whether it is known.
func sourceTier(source string) (int, bool) {
	t, ok := knownSources[source]
	return t, ok
}

var (
	htmlTag    = regexp.MustCompile(`<[^>]*>`)
	wsRun      = regexp.MustCompile(`\s+`)
	firstInt   = regexp.MustCompile(`\d+`)
	priceNum   = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	priceCur   = regexp.MustCompile(`[A-Za-z]{3}`)
	slugStrip  = regexp.MustCompile(`[^a-z0-9]+`)
	countryISO = map[string]string{
		// codes
		"it": "IT", "at": "AT", "de": "DE", "mx": "MX", "us": "US", "gb": "GB",
		"es": "ES", "fr": "FR", "ch": "CH",
		// names (incl. German/native spellings seen in the data)
		"italy": "IT", "italia": "IT", "italien": "IT",
		"austria": "AT", "österreich": "AT", "osterreich": "AT",
		"germany": "DE", "deutschland": "DE",
		"mexico": "MX", "méxico": "MX",
		"united states": "US", "usa": "US",
		"united kingdom": "GB", "uk": "GB",
		"spain": "ES", "españa": "ES",
		"france": "FR", "switzerland": "CH",
	}
	// allowedCurrencies is the ISO-4217 allow-list. An unknown currency is a
	// field-level problem, not silently accepted.
	allowedCurrencies = map[string]bool{
		"EUR": true, "USD": true, "GBP": true, "CHF": true, "MXN": true, "JPY": true,
	}
)

// normalizeRecord runs phases 1–2 for one raw record: parse & coerce each field,
// then validate invariants on the assembled Hotel. It returns the canonical
// hotel plus any fatal errors (record must be dropped) and soft warnings (record
// kept). Required fields (source, name, location) are FATAL when missing or
// unresolvable; optional fields (stars, price, amenities, coords) are repaired
// to a safe value and reported as warnings.
func normalizeRecord(index int, raw ent.RawHotel) (ent.Hotel, []ent.RecordError, []ent.RecordWarning) {
	var errs []ent.RecordError
	var warns []ent.RecordWarning
	fail := func(field, msg string) {
		errs = append(errs, ent.RecordError{Index: index, Source: raw.Source, Field: field, Message: msg})
	}
	warn := func(field, code, msg string) {
		warns = append(warns, ent.RecordWarning{Index: index, Field: field, Code: code, Message: msg})
	}

	// --- source (required, determines trust tier) ---
	if _, ok := sourceTier(raw.Source); !ok {
		fail("source", fmt.Sprintf("unknown source %q", raw.Source))
		return ent.Hotel{}, errs, warns
	}

	// --- name (required) ---
	name, _ := sanitizeText(ptr(firstNonEmpty(raw.HotelName, raw.Name)), maxNameLen)
	if name == "" {
		fail("name", "hotel name is required")
		return ent.Hotel{}, errs, warns
	}

	// --- location (required): explicit city+country, else split the location string ---
	city, country, locOK := resolveLocation(raw)
	if !locOK {
		fail("location", "could not resolve city and ISO country")
		return ent.Hotel{}, errs, warns
	}
	city, _ = sanitizeText(&city, maxCityLen)

	hotel := ent.Hotel{
		ID:        hotelID(name, city, country),
		Name:      name,
		City:      city,
		Country:   country,
		Amenities: make([]string, 0),
		Sources:   []string{raw.Source},
	}

	// --- stars (optional): int, "N stars", or null; out-of-range => drop + warn ---
	if stars, present, err := parseStars(firstRaw(raw.Stars, raw.Rating)); present {
		if err != nil {
			warn("stars", "stars_invalid", err.Error())
		} else {
			hotel.Stars = stars
		}
	}

	// --- description (optional): strip HTML, sanitize; never fatal ---
	if raw.Description != nil {
		desc, truncated := sanitizeText(ptr(stripHTML(*raw.Description)), maxDescLen)
		hotel.Description = desc
		if truncated {
			warn("description", "description_truncated", "description exceeded max length and was truncated")
		}
	}

	// --- amenities (optional): comma string | array | null => canonical set ---
	amen, err := parseAmenities(firstRaw(raw.Amenities, raw.Features))
	if err != nil {
		warn("amenities", "amenities_invalid", err.Error())
	}
	hotel.Amenities = amen

	// --- price (optional): EUR number, or "<amount> <CUR>" string ---
	if price, present, err := parsePrice(raw); present {
		if err != nil {
			warn("price", "price_invalid", err.Error())
		} else {
			hotel.Price = price
		}
	}

	// --- coordinates (optional): validate range, flag implausible location ---
	if raw.Coords != nil {
		coord, cerr, cwarn := validateCoords(*raw.Coords, city, country)
		if cerr != "" {
			warn("coordinates", "coordinates_invalid", cerr)
		} else {
			hotel.Coordinates = coord
			if cwarn != "" {
				warn("coordinates", "coordinates_implausible", cwarn)
			}
		}
	}

	// Stamp the now-known id onto warnings for this record.
	for i := range warns {
		warns[i].HotelID = hotel.ID
	}
	return hotel, errs, warns
}

// firstNonEmpty returns the first non-nil, non-blank string among the pointers.
func firstNonEmpty(ptrs ...*string) string {
	for _, p := range ptrs {
		if p != nil && strings.TrimSpace(*p) != "" {
			return *p
		}
	}
	return ""
}

// firstRaw returns the first non-empty json.RawMessage (a provider populates one
// of two alternative keys, e.g. stars vs rating).
func firstRaw(msgs ...json.RawMessage) json.RawMessage {
	for _, m := range msgs {
		if len(m) > 0 && strings.TrimSpace(string(m)) != "null" {
			return m
		}
	}
	return nil
}

func ptr(s string) *string { return &s }

// resolveLocation derives (city, ISO country) from either explicit city+country
// fields or a combined "City, Country" location string. Returns ok=false when it
// cannot resolve both.
func resolveLocation(raw ent.RawHotel) (city, country string, ok bool) {
	if raw.City != nil && strings.TrimSpace(*raw.City) != "" && raw.Country != nil {
		if iso, cok := normalizeCountry(*raw.Country); cok {
			return strings.TrimSpace(*raw.City), iso, true
		}
		return "", "", false
	}
	if raw.Location != nil {
		return splitLocation(*raw.Location)
	}
	return "", "", false
}

// splitLocation parses "Rimini, Italien" into ("Rimini", "IT"). It requires at
// least a city and a resolvable country separated by a comma.
func splitLocation(loc string) (city, country string, ok bool) {
	parts := strings.Split(loc, ",")
	if len(parts) < 2 {
		return "", "", false
	}
	city = strings.TrimSpace(parts[0])
	iso, cok := normalizeCountry(parts[len(parts)-1])
	if city == "" || !cok {
		return "", "", false
	}
	return city, iso, true
}

// normalizeCountry maps a code or a (possibly localized) country name to an
// ISO-3166 alpha-2 code. Unknown input returns ok=false.
func normalizeCountry(in string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(in))
	if iso, ok := countryISO[key]; ok {
		return iso, true
	}
	return "", false
}

// parseStars interprets the stars/rating field. It accepts an int, a string like
// "3 stars", or null/absent. present reports whether the provider sent anything;
// err is non-nil when a value was sent but is not a valid 1–5 integer (decimals
// are rejected — Stars is an official classification, never averaged).
func parseStars(raw json.RawMessage) (stars *int, present bool, err error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if s := strings.TrimSpace(string(raw)); s == "null" {
		return nil, false, nil
	}

	var n int
	if e := json.Unmarshal(raw, &n); e == nil {
		return clampStars(n)
	}

	var str string
	if e := json.Unmarshal(raw, &str); e == nil {
		m := firstInt.FindString(str)
		if m == "" {
			return nil, true, fmt.Errorf("stars %q is not numeric", str)
		}
		v, _ := strconv.Atoi(m)
		return clampStars(v)
	}

	return nil, true, fmt.Errorf("stars value not recognised: %s", strings.TrimSpace(string(raw)))
}

func clampStars(n int) (*int, bool, error) {
	if n < 1 || n > 5 {
		return nil, true, fmt.Errorf("stars %d out of range 1-5", n)
	}
	v := n
	return &v, true, nil
}

// parsePrice picks whichever price shape the provider sent: price_from_eur is a
// bare number in EUR; price_from is a "<amount> <CUR>" string.
func parsePrice(raw ent.RawHotel) (price *ent.Money, present bool, err error) {
	if m := firstRaw(raw.PriceFromEUR); m != nil {
		return parsePriceNumber(m, "EUR")
	}
	if m := firstRaw(raw.PriceFrom); m != nil {
		return parsePriceString(m)
	}
	return nil, false, nil
}

func parsePriceNumber(raw json.RawMessage, currency string) (*ent.Money, bool, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, true, fmt.Errorf("price not a number: %s", strings.TrimSpace(string(raw)))
	}
	return buildMoney(f, currency)
}

func parsePriceString(raw json.RawMessage) (*ent.Money, bool, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, true, fmt.Errorf("price not a string: %s", strings.TrimSpace(string(raw)))
	}
	numStr := priceNum.FindString(s)
	curStr := strings.ToUpper(priceCur.FindString(s))
	if numStr == "" || curStr == "" {
		return nil, true, fmt.Errorf("price %q missing amount or currency", s)
	}
	f, _ := strconv.ParseFloat(numStr, 64)
	return buildMoney(f, curStr)
}

func buildMoney(amount float64, currency string) (*ent.Money, bool, error) {
	if !allowedCurrencies[currency] {
		return nil, true, fmt.Errorf("unknown currency %q", currency)
	}
	if amount <= 0 {
		return nil, true, fmt.Errorf("price must be positive, got %v", amount)
	}
	cents := int(math.Round(amount * 100))
	if cents > maxPriceCents {
		return nil, true, fmt.Errorf("price %v %s exceeds plausible maximum", amount, currency)
	}
	return &ent.Money{AmountCents: cents, Currency: currency}, true, nil
}

// parseAmenities accepts a comma-separated string, a JSON array, or null, and
// returns the canonical, deduped, sorted amenity set. Unparseable JSON returns an
// error (and an empty set); individual junk entries are dropped silently.
func parseAmenities(raw json.RawMessage) ([]string, error) {
	out := make([]string, 0)
	if len(raw) == 0 {
		return out, nil
	}

	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		var s string
		if err2 := json.Unmarshal(raw, &s); err2 != nil {
			return out, fmt.Errorf("amenities not a string or array: %s", strings.TrimSpace(string(raw)))
		}
		items = strings.Split(s, ",")
	}

	set := map[string]bool{}
	for _, it := range items {
		c := canonicalizeAmenity(it)
		if c == "" || set[c] {
			continue
		}
		set[c] = true
		out = append(out, c)
		if len(out) >= maxAmenities {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

// amenitySynonyms maps known provider vocabulary onto a canonical token. Unknown
// values fall through to a slugified form so nothing is silently lost.
var amenitySynonyms = map[string]string{
	"wifi": "wifi", "free wifi": "wifi", "wi-fi": "wifi", "free wi-fi": "wifi", "wlan": "wifi",
	"pool": "pool", "swimming pool": "pool", "pools": "pool", "3 pools": "pool",
	"outdoor pool": "pool", "indoor pool": "pool",
	"parking": "parking", "free parking": "parking", "car park": "parking",
	"pets allowed": "pets", "pets": "pets", "pet friendly": "pets", "pet-friendly": "pets",
	"spa": "spa", "wellness": "spa",
	"kids club": "kids_club", "kids' club": "kids_club", "children's club": "kids_club",
	"breakfast": "breakfast", "breakfast included": "breakfast",
	"frühstück": "breakfast", "fruhstuck": "breakfast",
	"swim-up bar": "swim_up_bar", "swim up bar": "swim_up_bar",
}

func canonicalizeAmenity(in string) string {
	key := strings.ToLower(strings.TrimSpace(in))
	if key == "" {
		return ""
	}
	if c, ok := amenitySynonyms[key]; ok {
		return c
	}
	// Unknown amenity: slugify (lowercase, non-alphanumeric runs -> "_").
	slug := strings.Trim(slugStrip.ReplaceAllString(key, "_"), "_")
	if len(slug) > maxAmenityLen {
		slug = slug[:maxAmenityLen]
	}
	return slug
}

// stripHTML removes tags so stored descriptions are plain text. Combined with
// output encoding this neutralises stored-XSS from marketing HTML like <b>…</b>.
func stripHTML(s string) string {
	return htmlTag.ReplaceAllString(s, "")
}

// sanitizeText makes an untrusted string safe to store: coerce to valid UTF-8,
// drop control characters, collapse whitespace, trim, and cap length. It does NOT
// attempt to detect SQL/prompt injection — such content is stored verbatim as
// DATA (the repo's parameterized queries and the caller's treat-as-data
// discipline are the real defenses). Returns the cleaned value and whether it was
// truncated.
func sanitizeText(in *string, max int) (string, bool) {
	if in == nil {
		return "", false
	}
	s := strings.ToValidUTF8(*in, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(wsRun.ReplaceAllString(s, " "))

	truncated := false
	if runes := []rune(s); len(runes) > max {
		s = string(runes[:max])
		truncated = true
	}
	return s, truncated
}

// hotelID is a stable identifier derived from the normalized name+city+country.
// It is the id for the FIRST record of a dedup group; later fuzzy-matched records
// adopt the matched hotel's id rather than minting a new one.
func hotelID(name, city, country string) string {
	key := strings.ToLower(strings.TrimSpace(name)) + "|" +
		strings.ToLower(strings.TrimSpace(city)) + "|" + country
	sum := sha1.Sum([]byte(key))
	return fmt.Sprintf("%x", sum)
}
