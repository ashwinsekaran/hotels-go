package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashwinsekaran/hotels-go/ent"
	"github.com/ashwinsekaran/hotels-go/repo"
	"github.com/ashwinsekaran/hotels-go/uc"

	"github.com/julienschmidt/httprouter"
)

// newRouter wires real use-cases over a real in-memory repo (integration-style),
// exactly as main does minus the cross-cutting middleware. This is the handler
// DI seam: the use-case function is injected into each handler constructor.
func newRouter() (*httprouter.Router, uc.HotelStore) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := repo.NewHotelRepo()
	r := httprouter.New()
	r.Handle("POST", "/hotels", IngestHotelsHandler(logger, uc.MakeIngestHotelsUc(store)))
	r.Handle("GET", "/hotels", ListHotelsHandler(logger, uc.MakeListHotelsUc(store)))
	r.Handle("GET", "/hotels/:id", GetHotelHandler(logger, uc.MakeGetHotelUc(store)))
	return r, store
}

func do(t *testing.T, r *httprouter.Router, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const validFeed = `[
  {"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","stars":4,"amenities":"pool,wifi"},
  {"source":"partner-feed-b","hotel_name":"Alpenhof Garni","city":"Innsbruck","country":"AT"}
]`

func TestPostHotelsOK(t *testing.T) {
	r, _ := newRouter()
	w := do(t, r, "POST", "/hotels", validFeed)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var res uc.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(res.Normalized) != 2 {
		t.Fatalf("want 2 normalized, got %d", len(res.Normalized))
	}
}

func TestPostHotelsPartialFailureIs207(t *testing.T) {
	r, _ := newRouter()
	feed := `[
	  {"source":"mystery","hotel_name":"Ghost","city":"Rimini","country":"IT"},
	  {"source":"partner-feed-a","hotel_name":"Good Hotel","city":"Rimini","country":"IT"}
	]`
	w := do(t, r, "POST", "/hotels", feed)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", w.Code, w.Body.String())
	}
}

func TestPostHotelsAllFailedIs400(t *testing.T) {
	r, _ := newRouter()
	feed := `[{"source":"mystery","hotel_name":"Ghost","city":"Rimini","country":"IT"}]`
	w := do(t, r, "POST", "/hotels", feed)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostHotelsMalformedJSONIs400(t *testing.T) {
	r, _ := newRouter()
	w := do(t, r, "POST", "/hotels", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetHotelsWithFilter(t *testing.T) {
	r, _ := newRouter()
	do(t, r, "POST", "/hotels", validFeed)

	w := do(t, r, "GET", "/hotels?country=AT", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var hotels []ent.Hotel
	_ = json.Unmarshal(w.Body.Bytes(), &hotels)
	if len(hotels) != 1 || hotels[0].City != "Innsbruck" {
		t.Fatalf("country filter wrong: %+v", hotels)
	}

	// min_stars filter: only the 4-star Rimini hotel qualifies.
	w = do(t, r, "GET", "/hotels?min_stars=4", "")
	_ = json.Unmarshal(w.Body.Bytes(), &hotels)
	if len(hotels) != 1 || hotels[0].City != "Rimini" {
		t.Fatalf("min_stars filter wrong: %+v", hotels)
	}
}

func TestGetHotelsBadMinStarsIs400(t *testing.T) {
	r, _ := newRouter()
	w := do(t, r, "GET", "/hotels?min_stars=lots", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetHotelByIDHitAndMiss(t *testing.T) {
	r, _ := newRouter()
	do(t, r, "POST", "/hotels", validFeed)

	// Discover a real id from the list.
	w := do(t, r, "GET", "/hotels", "")
	var hotels []ent.Hotel
	_ = json.Unmarshal(w.Body.Bytes(), &hotels)
	if len(hotels) == 0 {
		t.Fatal("no hotels to fetch")
	}

	hit := do(t, r, "GET", "/hotels/"+hotels[0].ID, "")
	if hit.Code != http.StatusOK {
		t.Fatalf("hit status = %d, want 200", hit.Code)
	}
	miss := do(t, r, "GET", "/hotels/does-not-exist", "")
	if miss.Code != http.StatusNotFound {
		t.Fatalf("miss status = %d, want 404", miss.Code)
	}
}
