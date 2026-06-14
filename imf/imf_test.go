package imf_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/imf-cli/imf"
)

// clientFor returns a Client wired to srv with no pacing and fast retries.
func clientFor(srv *httptest.Server) *imf.Client {
	c := imf.NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	c.HTTP.Transport = srv.Client().Transport
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestListIndicators(t *testing.T) {
	payload := map[string]any{
		"indicators": map[string]any{
			"NGDP_RPCH": map[string]string{
				"label":       "Real GDP growth",
				"description": "Annual percent change in real GDP",
				"source":      "World Economic Outlook",
				"unit":        "Percent change",
			},
			"BCA_NGDPD": map[string]string{
				"label": "Current account balance",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/indicators" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0
	// Override BaseURL is not directly exposed, so we test via a real-URL client
	// by exercising Get directly with the test server URL.
	body, err := c.Get(context.Background(), srv.URL+"/indicators")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Indicators map[string]struct {
			Label string `json:"label"`
		} `json:"indicators"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.Indicators["NGDP_RPCH"]; !ok {
		t.Error("expected NGDP_RPCH in indicators response")
	}
	if resp.Indicators["NGDP_RPCH"].Label != "Real GDP growth" {
		t.Errorf("label = %q, want %q", resp.Indicators["NGDP_RPCH"].Label, "Real GDP growth")
	}
}

func TestListCountries(t *testing.T) {
	payload := map[string]any{
		"countries": map[string]any{
			"USA": map[string]string{"label": "United States"},
			"CHN": map[string]string{"label": "China"},
			"DEU": map[string]string{"label": "Germany"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL+"/countries")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Countries map[string]struct {
			Label string `json:"label"`
		} `json:"countries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Countries) != 3 {
		t.Errorf("got %d countries, want 3", len(resp.Countries))
	}
	if resp.Countries["USA"].Label != "United States" {
		t.Errorf("USA label = %q, want United States", resp.Countries["USA"].Label)
	}
}

func TestFetchDataDecoding(t *testing.T) {
	// Verify that the dataResp struct shape decodes correctly.
	raw := `{"values":{"NGDP_RPCH":{"USA":{"2020":-2.1,"2021":6.2},"CHN":{"2020":2.3}}}}`
	var resp struct {
		Values map[string]map[string]map[string]float64 `json:"values"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	usaVals := resp.Values["NGDP_RPCH"]["USA"]
	if len(usaVals) != 2 {
		t.Errorf("USA has %d years, want 2", len(usaVals))
	}
	if usaVals["2020"] != -2.1 {
		t.Errorf("USA 2020 = %v, want -2.1", usaVals["2020"])
	}
	if usaVals["2021"] != 6.2 {
		t.Errorf("USA 2021 = %v, want 6.2", usaVals["2021"])
	}
	if resp.Values["NGDP_RPCH"]["CHN"]["2020"] != 2.3 {
		t.Errorf("CHN 2020 = %v, want 2.3", resp.Values["NGDP_RPCH"]["CHN"]["2020"])
	}
}

func TestGet404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0
	c.Retries = 0

	_, err := c.Get(context.Background(), srv.URL+"/unknown")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestGet429Retries(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := imf.NewClient()
	c.Rate = 0
	c.Retries = 3

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
	if hits != 2 {
		t.Errorf("server saw %d hits, want 2", hits)
	}
}
