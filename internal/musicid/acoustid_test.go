package musicid

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewAcoustIDClient_MissingKey(t *testing.T) {
	_, err := NewAcoustIDClient("")
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestIdentify_SuccessfulMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.FormValue("client"); got != "test-key" {
			t.Errorf("client = %q, want %q", got, "test-key")
		}
		if got := r.FormValue("duration"); got != "30" {
			t.Errorf("duration = %q, want %q", got, "30")
		}
		if got := r.FormValue("meta"); got != "recordings releasegroups" {
			t.Errorf("meta = %q, want %q", got, "recordings releasegroups")
		}

		resp := acoustIDResponse{
			Status: "ok",
			Results: []acoustIDResult{
				{
					ID:    "result-1",
					Score: 0.95,
					Recordings: []acoustIDRecording{
						{
							ID:    "mbid-abc-123",
							Title: "Bohemian Rhapsody",
							Artists: []acoustIDArtist{
								{ID: "artist-1", Name: "Queen"},
							},
							ReleaseGroups: []acoustIDRelGroup{
								{ID: "rg-1", Title: "A Night at the Opera", Type: "Album"},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	info, err := client.Identify("AQAA...", 30)
	if err != nil {
		t.Fatalf("Identify failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil SongInfo")
	}
	if info.Title != "Bohemian Rhapsody" {
		t.Errorf("Title = %q, want %q", info.Title, "Bohemian Rhapsody")
	}
	if info.Artist != "Queen" {
		t.Errorf("Artist = %q, want %q", info.Artist, "Queen")
	}
	if info.Album != "A Night at the Opera" {
		t.Errorf("Album = %q, want %q", info.Album, "A Night at the Opera")
	}
	if info.MBID != "mbid-abc-123" {
		t.Errorf("MBID = %q, want %q", info.MBID, "mbid-abc-123")
	}
	if info.Score != 0.95 {
		t.Errorf("Score = %f, want %f", info.Score, 0.95)
	}
}

func TestIdentify_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := acoustIDResponse{
			Status:  "ok",
			Results: []acoustIDResult{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	info, err := client.Identify("AQAA...", 30)
	if err != nil {
		t.Fatalf("Identify failed: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil SongInfo for no results, got %+v", info)
	}
}

func TestIdentify_LowConfidence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := acoustIDResponse{
			Status: "ok",
			Results: []acoustIDResult{
				{
					ID:    "result-low",
					Score: 0.2, // Below minConfidence threshold.
					Recordings: []acoustIDRecording{
						{
							ID:    "mbid-low",
							Title: "Some Song",
							Artists: []acoustIDArtist{
								{Name: "Some Artist"},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	info, err := client.Identify("AQAA...", 30)
	if err != nil {
		t.Fatalf("Identify failed: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil for low-confidence result, got %+v", info)
	}
}

func TestIdentify_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := acoustIDResponse{
			Status: "error",
			Error:  acoustIDError{Code: 4, Message: "invalid API key"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("bad-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	_, err = client.Identify("AQAA...", 30)
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestIdentify_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	_, err = client.Identify("AQAA...", 30)
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
}

func TestIdentify_EmptyFingerprint(t *testing.T) {
	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Identify("", 30)
	if err == nil {
		t.Fatal("expected error for empty fingerprint")
	}
}

func TestIdentify_InvalidDuration(t *testing.T) {
	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Identify("AQAA...", 0)
	if err == nil {
		t.Fatal("expected error for zero duration")
	}
}

func TestRateLimiting(t *testing.T) {
	var mu sync.Mutex
	var calls []time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, time.Now())
		mu.Unlock()

		resp := acoustIDResponse{Status: "ok", Results: []acoustIDResult{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewAcoustIDClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = srv.URL

	// Fire 3 requests rapidly. The rate limiter should space them out.
	for i := 0; i < 3; i++ {
		_, err := client.Identify("AQAA...", 30)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	// Verify that the gap between consecutive calls is at least close to
	// rateLimitInterval. Allow 50ms tolerance for scheduling jitter.
	for i := 1; i < len(calls); i++ {
		gap := calls[i].Sub(calls[i-1])
		minGap := rateLimitInterval - 50*time.Millisecond
		if gap < minGap {
			t.Errorf("gap between call %d and %d was %v, expected >= %v", i-1, i, gap, rateLimitInterval)
		}
	}
}
