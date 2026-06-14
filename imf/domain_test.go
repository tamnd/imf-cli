package imf

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in imf_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "imf" {
		t.Errorf("Scheme = %q, want imf", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "imf" {
		t.Errorf("Identity.Binary = %q, want imf", info.Identity.Binary)
	}
}

func TestClassifyIndicator(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"NGDP_RPCH", "indicator", "NGDP_RPCH"},
		{"BCA_NGDPD", "indicator", "BCA_NGDPD"},
		{"PCPIPCH", "indicator", "PCPIPCH"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyCountry(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"USA", "country", "USA"},
		{"CHN", "country", "CHN"},
		{"DEU", "country", "DEU"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyUnrecognized(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
	_, _, err = Domain{}.Classify("not valid")
	if err == nil {
		t.Error("Classify(\"not valid\") should return an error")
	}
}

func TestLocateIndicator(t *testing.T) {
	got, err := Domain{}.Locate("indicator", "NGDP_RPCH")
	want := "https://" + Host + "/external/datamapper/NGDP_RPCH"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateCountry(t *testing.T) {
	got, err := Domain{}.Locate("country", "USA")
	want := "https://" + Host + "/en/Countries/USA/"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "X")
	if err == nil {
		t.Error("Locate with unknown type should return an error")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the round trip:
// a record mints to its URI, its body is readable, and a bare id resolves back
// to the same URI. The init in domain.go registers the domain, so kit.Open finds it.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	ind := &Indicator{
		ID:    "NGDP_RPCH",
		Label: "Real GDP growth",
	}
	u, err := h.Mint(ind)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "imf://indicator/NGDP_RPCH"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	country := &Country{Code: "USA", Label: "United States"}
	u2, err := h.Mint(country)
	if err != nil {
		t.Fatalf("Mint country: %v", err)
	}
	if want := "imf://country/USA"; u2.String() != want {
		t.Errorf("Mint country = %q, want %q", u2.String(), want)
	}
}
