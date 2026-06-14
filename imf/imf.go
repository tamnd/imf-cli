// Package imf is the library behind the imf command line:
// the HTTP client, request shaping, and the typed data models for the IMF DataMapper API.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
// Build your endpoint calls and JSON decoding on top of it.
package imf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// DefaultUserAgent identifies the client to the IMF API.
const DefaultUserAgent = "imf-cli/dev (+https://github.com/tamnd/imf-cli)"

// Host is the site this client talks to.
const Host = "www.imf.org"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host + "/external/datamapper/api/v1"

// Client talks to the IMF DataMapper API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 500ms
// minimum gap between requests (IMF is picky), and five retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      500 * time.Millisecond,
		Retries:   5,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- typed output records ---

// Indicator is one IMF data series, e.g. "Real GDP growth" (NGDP_RPCH).
type Indicator struct {
	ID          string `kit:"id" json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Unit        string `json:"unit"`
}

// Country is one IMF country/region entry.
type Country struct {
	Code  string `kit:"id" json:"code"`
	Label string `json:"label"`
}

// DataPoint is one value: the reading for a given indicator, country and year.
type DataPoint struct {
	Indicator string  `kit:"id" json:"indicator"`
	Country   string  `json:"country"`
	Year      string  `json:"year"`
	Value     float64 `json:"value"`
}

// --- API calls ---

// indicatorsResp is the raw JSON envelope from /indicators.
type indicatorsResp struct {
	Indicators map[string]struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		Source      string `json:"source"`
		Unit        string `json:"unit"`
	} `json:"indicators"`
}

// ListIndicators fetches all available indicator codes and their metadata.
func (c *Client) ListIndicators(ctx context.Context) ([]*Indicator, error) {
	body, err := c.Get(ctx, BaseURL+"/indicators")
	if err != nil {
		return nil, err
	}
	var resp indicatorsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode indicators: %w", err)
	}
	// Collect and sort alphabetically by ID for stable output.
	ids := make([]string, 0, len(resp.Indicators))
	for id := range resp.Indicators {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]*Indicator, 0, len(ids))
	for _, id := range ids {
		m := resp.Indicators[id]
		out = append(out, &Indicator{
			ID:          id,
			Label:       m.Label,
			Description: m.Description,
			Source:      m.Source,
			Unit:        m.Unit,
		})
	}
	return out, nil
}

// countriesResp is the raw JSON envelope from /countries.
type countriesResp struct {
	Countries map[string]struct {
		Label string `json:"label"`
	} `json:"countries"`
}

// ListCountries fetches all country/region codes and labels.
func (c *Client) ListCountries(ctx context.Context) ([]*Country, error) {
	body, err := c.Get(ctx, BaseURL+"/countries")
	if err != nil {
		return nil, err
	}
	var resp countriesResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode countries: %w", err)
	}
	codes := make([]string, 0, len(resp.Countries))
	for code := range resp.Countries {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	out := make([]*Country, 0, len(codes))
	for _, code := range codes {
		out = append(out, &Country{Code: code, Label: resp.Countries[code].Label})
	}
	return out, nil
}

// dataResp is the raw JSON envelope from /INDICATOR or /INDICATOR/COUNTRY.
type dataResp struct {
	Values map[string]map[string]map[string]float64 `json:"values"`
}

// FetchData fetches data points for indicator. If country is non-empty, only
// that country's data is fetched. periods is an optional comma-separated list
// of years to filter by (appended as ?periods=...).
func (c *Client) FetchData(ctx context.Context, indicator, country, periods string) ([]*DataPoint, error) {
	u := BaseURL + "/" + indicator
	if country != "" {
		u += "/" + country
	}
	if periods != "" {
		u += "?periods=" + periods
	}

	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp dataResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode data: %w", err)
	}

	// values: { "NGDP_RPCH": { "USA": { "2020": -2.1, ... }, ... } }
	var out []*DataPoint
	for indCode, countriesMap := range resp.Values {
		for cCode, yearsMap := range countriesMap {
			years := make([]string, 0, len(yearsMap))
			for yr := range yearsMap {
				years = append(years, yr)
			}
			sort.Strings(years)
			for _, yr := range years {
				out = append(out, &DataPoint{
					Indicator: indCode,
					Country:   cCode,
					Year:      yr,
					Value:     yearsMap[yr],
				})
			}
		}
	}
	// Sort: by country then year for stable, readable output.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Country != out[j].Country {
			return out[i].Country < out[j].Country
		}
		return out[i].Year < out[j].Year
	})
	return out, nil
}
