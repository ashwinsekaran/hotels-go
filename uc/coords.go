package uc

import (
	"fmt"
	"math"
	"strings"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// coordPlausibilityKm is the distance beyond which supplied coordinates are
// considered inconsistent with the hotel's stated city centroid and flagged as a
// warning (never rejected).
const coordPlausibilityKm = 150.0

// cityCentroids is a small, deliberately limited reference table keyed by
// "city|ISO" (lowercased city). It exists to demonstrate the plausibility check —
// e.g. catching the City Lodge Berlin record whose coordinates actually point at
// Munich. Unknown cities are simply not checked (no false warnings).
//
// TODO(geocoding-reference): replace this hand-seeded table with a real gazetteer
// for full coverage. Geocoding to FILL missing coords was intentionally NOT
// implemented (absent coords stay nil).
var cityCentroids = map[string]ent.GeoPoint{
	"rimini|IT":           {Lat: 44.0594, Lng: 12.5683},
	"innsbruck|AT":        {Lat: 47.2692, Lng: 11.4041},
	"berlin|DE":           {Lat: 52.5200, Lng: 13.4050},
	"munich|DE":           {Lat: 48.1351, Lng: 11.5820},
	"playa del carmen|MX": {Lat: 20.6296, Lng: -87.0739},
}

// validateCoords range-checks coordinates and, when the city is in the reference
// table, flags coordinates that are implausibly far from the city centroid.
// Returns the point (nil if the range check failed), a fatal-ish cerr string
// (coordinate dropped), and a soft cwarn string (coordinate kept but flagged).
func validateCoords(p ent.GeoPoint, city, country string) (point *ent.GeoPoint, cerr, cwarn string) {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || math.IsInf(p.Lat, 0) || math.IsInf(p.Lng, 0) {
		return nil, "coordinates are NaN or Inf", ""
	}
	if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
		return nil, fmt.Sprintf("coordinates out of range: lat=%v lng=%v", p.Lat, p.Lng), ""
	}

	coord := p
	key := strings.ToLower(strings.TrimSpace(city)) + "|" + country
	if centroid, ok := cityCentroids[key]; ok {
		if d := haversineKm(coord, centroid); d > coordPlausibilityKm {
			return &coord, "", fmt.Sprintf("coordinates %.0f km from %s centroid — likely wrong", d, city)
		}
	}
	return &coord, "", ""
}

// haversineKm returns the great-circle distance between two points in kilometers.
func haversineKm(a, b ent.GeoPoint) float64 {
	const earthKm = 6371.0
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLng := (b.Lng - a.Lng) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthKm * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}
