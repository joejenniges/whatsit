package musicid

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	defaultAcoustIDURL = "https://api.acoustid.org/v2/lookup"

	// WHY 3 req/sec not 3 req/min: AcoustID docs say "3 requests per second"
	// for identified API keys. We use a simple token bucket that refills at
	// that rate. If the key isn't registered for higher throughput, the server
	// returns 429 and we surface that as an error.
	rateLimitInterval = time.Second / 3

	// Minimum confidence to consider a match useful. AcoustID returns a score
	// 0.0-1.0. Below this threshold we treat it as no match rather than
	// returning garbage.
	minConfidence = 0.5
)

// SongInfo holds metadata for an identified song.
type SongInfo struct {
	Title  string
	Artist string
	Album  string
	MBID   string // MusicBrainz recording ID
	Score  float64
}

// AcoustIDClient queries the AcoustID web API to identify songs from audio fingerprints.
type AcoustIDClient struct {
	APIKey     string
	BaseURL    string // Override for testing; defaults to AcoustID production.
	HTTPClient *http.Client

	mu       sync.Mutex
	lastCall time.Time
}

// NewAcoustIDClient creates a client with the given API key.
// Returns an error if apiKey is empty.
func NewAcoustIDClient(apiKey string) (*AcoustIDClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("acoustid: API key is required (register at https://acoustid.org/new-application)")
	}
	return &AcoustIDClient{
		APIKey:  apiKey,
		BaseURL: defaultAcoustIDURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// Identify looks up a fingerprint against AcoustID and returns the best matching
// song, or nil if no match meets the confidence threshold. The fingerprint should
// be a base64-encoded Chromaprint string, and duration is in seconds.
func (c *AcoustIDClient) Identify(fingerprint string, duration int) (*SongInfo, error) {
	if fingerprint == "" {
		return nil, fmt.Errorf("acoustid: empty fingerprint")
	}
	if duration <= 0 {
		return nil, fmt.Errorf("acoustid: invalid duration %d", duration)
	}

	c.rateLimit()

	params := url.Values{
		"client":      {c.APIKey},
		"fingerprint": {fingerprint},
		"duration":    {strconv.Itoa(duration)},
		"meta":        {"recordings releasegroups"},
	}

	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = defaultAcoustIDURL
	}

	resp, err := c.HTTPClient.PostForm(baseURL, params)
	if err != nil {
		return nil, fmt.Errorf("acoustid: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("acoustid: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acoustid: HTTP %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("acoustid: raw response: %s", string(body[:min(500, len(body))]))

	var result acoustIDResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("acoustid: parsing response: %w", err)
	}

	if result.Status != "ok" {
		msg := result.Error.Message
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("acoustid: API error: %s", msg)
	}

	return bestMatch(result.Results), nil
}

// rateLimit ensures we don't exceed the AcoustID rate limit.
func (c *AcoustIDClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastCall)
	if elapsed < rateLimitInterval {
		time.Sleep(rateLimitInterval - elapsed)
	}
	c.lastCall = time.Now()
}

// bestMatch extracts the highest-confidence result with usable metadata.
func bestMatch(results []acoustIDResult) *SongInfo {
	for _, r := range results {
		if r.Score < minConfidence {
			continue
		}
		for _, rec := range r.Recordings {
			info := &SongInfo{
				Title: rec.Title,
				MBID:  rec.ID,
				Score: r.Score,
			}
			if len(rec.Artists) > 0 {
				info.Artist = rec.Artists[0].Name
			}
			if len(rec.ReleaseGroups) > 0 {
				info.Album = rec.ReleaseGroups[0].Title
			}
			// Return first recording with a title from the highest-scoring result.
			if info.Title != "" {
				return info
			}
		}
	}
	return nil
}

// --- AcoustID JSON response types ---

type acoustIDResponse struct {
	Status  string           `json:"status"`
	Error   acoustIDError    `json:"error"`
	Results []acoustIDResult `json:"results"`
}

type acoustIDError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type acoustIDResult struct {
	ID         string             `json:"id"`
	Score      float64            `json:"score"`
	Recordings []acoustIDRecording `json:"recordings"`
}

type acoustIDRecording struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Artists       []acoustIDArtist    `json:"artists"`
	ReleaseGroups []acoustIDRelGroup  `json:"releasegroups"`
}

type acoustIDArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type acoustIDRelGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}
