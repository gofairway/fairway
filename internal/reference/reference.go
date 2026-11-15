// Package reference provides mid-market foreign exchange reference rates
// to benchmark payment corridor execution on Stellar.
package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultSourceIdentifier is recorded in the measurements table.
const DefaultSourceIdentifier = "frankfurter.app"

// DefaultFrankfurterV2URL is the official API endpoint for Frankfurter v2,
// which provides daily ECB mid-market rates as well as global central bank rates (e.g. NGN via CBN).
const DefaultFrankfurterV2URL = "https://api.frankfurter.dev/v2/rates"

// Fetcher retrieves independent mid-market foreign exchange rates.
type Fetcher interface {
	FetchRate(ctx context.Context, fromCurrency, toCurrency string) (rate float64, src string, err error)
}

// assetCurrencies maps the Stellar asset codes whose underlying currency is not
// the code minus a trailing marker letter. Adding one here is explicit on
// purpose: a blanket rule over the last letter would turn unrelated codes into
// valid-looking currencies.
var assetCurrencies = map[string]string{
	"NGNC": "NGN",
	"USDC": "USD",
	"EURC": "EUR",
	"GHSC": "GHS",
	"KESC": "KES",
	"USDT": "USD",
	"EURT": "EUR",
	"AUDD": "AUD",
	"XSGD": "SGD",
	"IDRT": "IDR",
	"ARST": "ARS",
	"BRLA": "BRL",
	"XLM":  "XLM",
}

// AssetToCurrency maps a Stellar asset code to its underlying fiat / ISO currency code.
// E.g. NGNC -> NGN, USDC -> USD, EURC -> EUR.
func AssetToCurrency(code string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if currency, ok := assetCurrencies[upper]; ok {
		return currency
	}
	// The one inference kept: a 4-letter code ending in 'C' (e.g. BRLC) is the
	// fiat currency followed by the C for "custodial".
	if len(upper) == 4 && strings.HasSuffix(upper, "C") {
		return upper[:3]
	}
	return upper
}

// FrankfurterFetcher retrieves mid-market reference rates from Frankfurter.
type FrankfurterFetcher struct {
	endpoint   string
	sourceName string
	httpClient *http.Client
}

// NewFrankfurterFetcher creates a new reference rate fetcher.
// If endpoint is empty or points to api.frankfurter.app, DefaultFrankfurterV2URL is used.
func NewFrankfurterFetcher(endpoint string) *FrankfurterFetcher {
	ep := strings.TrimSpace(endpoint)
	if ep == "" || strings.Contains(ep, "api.frankfurter.app") {
		ep = DefaultFrankfurterV2URL
	}
	return &FrankfurterFetcher{
		endpoint:   ep,
		sourceName: DefaultSourceIdentifier,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type v2RateRecord struct {
	Date  string  `json:"date"`
	Base  string  `json:"base"`
	Quote string  `json:"quote"`
	Rate  float64 `json:"rate"`
}

// FetchRate returns the exchange rate (1 unit of fromCurrency in toCurrency) and the source name.
func (f *FrankfurterFetcher) FetchRate(ctx context.Context, fromCurrency, toCurrency string) (float64, string, error) {
	from := strings.ToUpper(strings.TrimSpace(fromCurrency))
	to := strings.ToUpper(strings.TrimSpace(toCurrency))

	if from == "" || to == "" {
		return 0, "", fmt.Errorf("from and to currencies cannot be empty")
	}

	// 1:1 if same currency
	if from == to {
		return 1.0, f.sourceName, nil
	}

	params := url.Values{}
	params.Set("base", strings.ToLower(from))
	params.Set("quotes", strings.ToLower(to))

	reqURL := fmt.Sprintf("%s?%s", f.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("building request for %s/%s: %w", from, to, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("fetching rate %s/%s: %w", from, to, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return 0, "", fmt.Errorf("reading response body for %s/%s: %w", from, to, err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("frankfurter returned HTTP %d for %s/%s: %s", resp.StatusCode, from, to, string(body))
	}

	var records []v2RateRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return 0, "", fmt.Errorf("decoding frankfurter response for %s/%s: %w (body: %s)", from, to, err, string(body))
	}

	for _, rec := range records {
		if strings.EqualFold(rec.Quote, to) {
			if rec.Rate <= 0 {
				return 0, "", fmt.Errorf("invalid rate %f returned for %s/%s", rec.Rate, from, to)
			}
			return rec.Rate, f.sourceName, nil
		}
	}

	return 0, "", fmt.Errorf("quote for %s not found in frankfurter response for %s", to, from)
}
