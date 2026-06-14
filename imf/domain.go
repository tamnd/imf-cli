package imf

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes imf as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/imf-cli/imf"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// imf:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone imf binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the imf driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "imf",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "imf",
			Short:  "A command line for the IMF DataMapper API.",
			Long: `A command line for the IMF DataMapper API.

imf reads public IMF economic data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/imf-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// indicator: fetch one indicator by code (resolver, seeds the mint index).
	kit.Handle(app, kit.OpMeta{
		Name:     "indicator",
		Group:    "read",
		Single:   true,
		Resolver: true,
		Summary:  "Fetch one indicator by code",
		URIType:  "indicator",
		Args:     []kit.Arg{{Name: "code", Help: "indicator code e.g. NGDP_RPCH"}},
	}, getIndicator)

	// indicators: list all available indicator codes and metadata.
	kit.Handle(app, kit.OpMeta{
		Name:    "indicators",
		Group:   "read",
		List:    true,
		Summary: "List all IMF indicators",
		URIType: "indicator",
	}, listIndicators)

	// country: fetch one country by code (resolver, seeds the mint index).
	kit.Handle(app, kit.OpMeta{
		Name:     "country",
		Group:    "read",
		Single:   true,
		Resolver: true,
		Summary:  "Fetch one country by code",
		URIType:  "country",
		Args:     []kit.Arg{{Name: "code", Help: "country ISO3 code e.g. USA"}},
	}, getCountry)

	// countries: list all country/region codes and labels.
	kit.Handle(app, kit.OpMeta{
		Name:    "countries",
		Group:   "read",
		List:    true,
		Summary: "List all IMF countries and regions",
		URIType: "country",
	}, listCountries)

	// data: fetch data points for a given indicator, optionally filtered by country and year range.
	kit.Handle(app, kit.OpMeta{
		Name:    "data",
		Group:   "read",
		List:    true,
		Summary: "Fetch data points for an indicator",
		URIType: "data",
		Args:    []kit.Arg{{Name: "indicator", Help: "indicator code e.g. NGDP_RPCH"}},
	}, fetchData)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type indicatorInput struct {
	Code   string  `kit:"arg" help:"indicator code e.g. NGDP_RPCH"`
	Client *Client `kit:"inject"`
}

type indicatorsInput struct {
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

type countryInput struct {
	Code   string  `kit:"arg" help:"country ISO3 code e.g. USA"`
	Client *Client `kit:"inject"`
}

type countriesInput struct {
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

type dataInput struct {
	Indicator string  `kit:"arg" help:"indicator code e.g. NGDP_RPCH"`
	Country   string  `kit:"flag" help:"country ISO3 code e.g. USA (omit for all countries)"`
	Periods   string  `kit:"flag" help:"comma-separated years e.g. 2020,2021,2022"`
	Client    *Client `kit:"inject"`
}

// --- handlers ---

func getIndicator(ctx context.Context, in indicatorInput, emit func(*Indicator) error) error {
	items, err := in.Client.ListIndicators(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, item := range items {
		if item.ID == in.Code {
			return emit(item)
		}
	}
	return errs.NotFound("indicator %q not found", in.Code)
}

func listIndicators(ctx context.Context, in indicatorsInput, emit func(*Indicator) error) error {
	items, err := in.Client.ListIndicators(ctx)
	if err != nil {
		return mapErr(err)
	}
	for i, item := range items {
		if in.Limit > 0 && i >= in.Limit {
			break
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return nil
}

func getCountry(ctx context.Context, in countryInput, emit func(*Country) error) error {
	items, err := in.Client.ListCountries(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, item := range items {
		if item.Code == in.Code {
			return emit(item)
		}
	}
	return errs.NotFound("country %q not found", in.Code)
}

func listCountries(ctx context.Context, in countriesInput, emit func(*Country) error) error {
	items, err := in.Client.ListCountries(ctx)
	if err != nil {
		return mapErr(err)
	}
	for i, item := range items {
		if in.Limit > 0 && i >= in.Limit {
			break
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return nil
}

func fetchData(ctx context.Context, in dataInput, emit func(*DataPoint) error) error {
	points, err := in.Client.FetchData(ctx, in.Indicator, in.Country, in.Periods)
	if err != nil {
		return mapErr(err)
	}
	for _, pt := range points {
		if err := emit(pt); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: URI driver string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
// Uppercase codes like "NGDP_RPCH" are indicators; 3-letter uppercase codes
// like "USA" are countries.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty IMF reference")
	}
	// 3-letter all-uppercase: country code
	if len(input) == 3 && input == strings.ToUpper(input) && isAlpha(input) {
		return "country", input, nil
	}
	// Uppercase with underscores: indicator code
	if input == strings.ToUpper(input) && containsUnderscore(input) {
		return "indicator", input, nil
	}
	// Bare uppercase code without underscore (e.g. "BCA")
	if input == strings.ToUpper(input) && isAlphaNum(input) {
		return "indicator", input, nil
	}
	return "", "", errs.Usage("unrecognized IMF reference: %q", input)
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "indicator":
		return "https://" + Host + "/external/datamapper/" + id, nil
	case "country":
		return "https://" + Host + "/en/Countries/" + id + "/", nil
	default:
		return "", errs.Usage("imf has no resource type %q", uriType)
	}
}

// --- helpers ---

func mapErr(err error) error {
	return err
}

func isAlpha(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func isAlphaNum(s string) bool {
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func containsUnderscore(s string) bool {
	return strings.ContainsRune(s, '_')
}
