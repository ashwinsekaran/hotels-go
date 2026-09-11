// Package repo is an infrastructure adapter that implements the use-case ports.
// It is an in-memory map and is structured so a database-backed type can replace
// it with NO change to any other layer: it satisfies uc.HotelStore STRUCTURALLY
// (by method set), so it does not import the uc package at all.
//
// To move to a real database, write a new type (e.g. PgHotelRepo) with the same
// method set and construct it in main instead of HotelRepo.
package repo

import (
	"sync"

	"github.com/ashwinsekaran/hotels-go/ent"
)

// HotelRepo is a concurrency-safe in-memory store keyed by canonical hotel id.
// The RWMutex makes it safe for the many goroutines an http.Server spawns; a real
// DB adapter would delegate locking to the database instead.
type HotelRepo struct {
	mu    sync.RWMutex
	store map[string]ent.Hotel
}

// NewHotelRepo returns an empty, ready-to-use in-memory HotelRepo.
func NewHotelRepo() *HotelRepo {
	return &HotelRepo{store: make(map[string]ent.Hotel)}
}

// GetById looks up a hotel by id, following Go's comma-ok idiom for "not found".
func (r *HotelRepo) GetById(id string) (ent.Hotel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.store[id]
	return h, ok
}

// Save writes or overwrites a hotel keyed by its ID (overwrite supports merge).
func (r *HotelRepo) Save(h ent.Hotel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[h.ID] = h
}

// All returns a snapshot slice of every stored hotel. The use-case layer uses it
// for listing and for dedup matching; map iteration order is unspecified, so
// callers that need a stable order sort the result themselves.
func (r *HotelRepo) All() []ent.Hotel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ent.Hotel, 0, len(r.store))
	for _, h := range r.store {
		out = append(out, h)
	}
	return out
}
