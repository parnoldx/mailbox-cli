package sieve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestUI(lists *Lists) *WebUI {
	return &WebUI{lists: lists, fetched: time.Now()}
}

func TestMutatingHandlersRejectGET(t *testing.T) {
	ui := newTestUI(NewLists())
	handler := ui.Handler()
	for _, path := range []string{"/add?list=feed&email=a@b.com", "/remove?list=feed&email=a@b.com", "/reload"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s = %d, want 405", path, rec.Code)
		}
	}
}

func TestAddRejectsUnsafeAddress(t *testing.T) {
	ui := newTestUI(NewLists())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, `/add?list=feed`, strings.NewReader(`email=a@b.com"]{discard;}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ui.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCurrentHandsOutCopies(t *testing.T) {
	ui := newTestUI(&Lists{Feed: []string{"a@b.com"}})
	mine, _, err := ui.current()
	if err != nil {
		t.Fatal(err)
	}
	mine.Feed = append(mine.Feed, "intruder@b.com")
	theirs, _, err := ui.current()
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs.Feed) != 1 {
		t.Fatalf("cache was mutated through the returned copy: %v", theirs.Feed)
	}
}

func TestConcurrentReadsDoNotRace(t *testing.T) {
	ui := newTestUI(&Lists{Feed: []string{"a@b.com"}, Blacklist: []string{"c@d.com"}})
	handler := ui.Handler()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("index = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()
}
