package repo

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ashwinsekaran/hotels-go/ent"
)

func TestSaveGetRoundTrip(t *testing.T) {
	r := NewHotelRepo()
	h := ent.Hotel{ID: "abc", Name: "Test", City: "Rimini", Country: "IT"}
	r.Save(h)

	got, ok := r.GetById("abc")
	if !ok || got.Name != "Test" {
		t.Fatalf("round-trip failed: %+v ok=%v", got, ok)
	}
	if _, ok := r.GetById("missing"); ok {
		t.Fatalf("expected not-found for missing id")
	}
}

func TestSaveOverwrites(t *testing.T) {
	r := NewHotelRepo()
	r.Save(ent.Hotel{ID: "x", Name: "First"})
	r.Save(ent.Hotel{ID: "x", Name: "Second"})
	if got, _ := r.GetById("x"); got.Name != "Second" {
		t.Fatalf("overwrite failed: %q", got.Name)
	}
	if len(r.All()) != 1 {
		t.Fatalf("overwrite should not grow the store")
	}
}

// Run with -race to prove the RWMutex protects concurrent access.
func TestConcurrentAccess(t *testing.T) {
	r := NewHotelRepo()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("h%d", n)
			r.Save(ent.Hotel{ID: id, Name: id})
			_, _ = r.GetById(id)
			_ = r.All()
		}(i)
	}
	wg.Wait()
	if len(r.All()) != 50 {
		t.Fatalf("want 50 hotels, got %d", len(r.All()))
	}
}
