// Package handlers is the HTTP delivery adapter (julienschmidt/httprouter). Each
// handler is a closure returned from a constructor that closes over a use-case
// function AND a *slog.Logger. Handlers own ALL HTTP concerns — body-size limits,
// path/query parsing, JSON encode/decode, status codes, and request-level
// logging — and delegate every business decision to the use-case. A handler
// knows nothing about storage or normalization; it never imports repo, and does
// no domain validation itself.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ashwinsekaran/hotels-go/ent"
	"github.com/ashwinsekaran/hotels-go/uc"

	"github.com/julienschmidt/httprouter"
	"go.opentelemetry.io/otel/trace"
)

const (
	// maxBodyBytes caps the ingest payload to bound memory (abuse defense).
	maxBodyBytes = 1 << 20 // 1 MiB
	// maxBatch caps the number of records per ingest request.
	maxBatch = 1000
)

// IngestHotelsHandler returns the POST /hotels handler. It decodes a raw provider
// array, delegates normalization+dedup+merge to ingestUc, and maps the outcome to
// a status code: 200 all-ok, 207 Multi-Status when some records failed but others
// succeeded, 400 when the body is malformed or every record failed.
func IngestHotelsHandler(logger *slog.Logger, ingestUc uc.IngestHotelsUc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		var raws []ent.RawHotel
		if err := json.NewDecoder(r.Body).Decode(&raws); err != nil {
			http.Error(w, "invalid JSON body: expected an array of hotel records", http.StatusBadRequest)
			return
		}
		if len(raws) > maxBatch {
			http.Error(w, "batch too large", http.StatusRequestEntityTooLarge)
			return
		}

		result := ingestUc(raws)

		status := http.StatusOK
		if len(result.Errors) > 0 {
			if len(result.Normalized) == 0 {
				status = http.StatusBadRequest
			} else {
				status = http.StatusMultiStatus
			}
		}

		sc := trace.SpanContextFromContext(r.Context())
		logger.InfoContext(r.Context(), "hotels ingested",
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
			"received", len(raws),
			"normalized", len(result.Normalized),
			"errors", len(result.Errors),
			"warnings", len(result.Warnings),
		)

		writeJSON(w, status, result)
	}
}

// ListHotelsHandler returns the GET /hotels handler with optional ?city=,
// ?country=, and ?min_stars= filters. A non-integer min_stars is a 400.
func ListHotelsHandler(logger *slog.Logger, listUc uc.ListHotelsUc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		q := r.URL.Query()
		filter := uc.HotelFilter{City: q.Get("city"), Country: q.Get("country")}
		if v := q.Get("min_stars"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				http.Error(w, "min_stars must be an integer", http.StatusBadRequest)
				return
			}
			filter.MinStars = &n
		}

		hotels := listUc(filter)

		sc := trace.SpanContextFromContext(r.Context())
		logger.InfoContext(r.Context(), "hotels listed",
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
			"count", len(hotels),
		)

		writeJSON(w, http.StatusOK, hotels)
	}
}

// GetHotelHandler returns the GET /hotels/:id handler. 404 when the id is unknown.
func GetHotelHandler(logger *slog.Logger, getUc uc.GetHotelUc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		id := p.ByName("id")
		hotel, ok := getUc(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		sc := trace.SpanContextFromContext(r.Context())
		logger.InfoContext(r.Context(), "hotel fetched",
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
			"hotel_id", id,
		)

		writeJSON(w, http.StatusOK, hotel)
	}
}

// writeJSON is the single place that sets the content type, status, and encodes
// the response body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
