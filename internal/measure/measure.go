// Package measure queries the Stellar Horizon pathfinding API to score corridors.
package measure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gofairway/fairway/internal/reference"
	"github.com/gofairway/fairway/internal/store"
)

// HorizonClient wraps Stellar Horizon strict-receive pathfinding and mid-market rate benchmarking.
type HorizonClient struct {
	baseURL    string
	refFetcher reference.Fetcher
	httpClient *http.Client
}

// NewHorizonClient creates a HorizonClient pointing at the given Horizon base URL
// and configured with a reference rate fetcher for mid-market benchmarking.
func NewHorizonClient(baseURL string, refFetcher reference.Fetcher) *HorizonClient {
	return &HorizonClient{
		baseURL:    baseURL,
		refFetcher: refFetcher,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// pathPaymentResponse is the subset of Horizon's find_paths (strict-send) response we care about.
type pathPaymentResponse struct {
	Embedded struct {
		Records []struct {
			SourceAmount      string `json:"source_amount"`
			DestinationAmount string `json:"destination_amount"`
			Path              []struct {
				AssetCode   string `json:"asset_code"`
				AssetIssuer string `json:"asset_issuer"`
				AssetType   string `json:"asset_type"`
			} `json:"path"`
		} `json:"records"`
	} `json:"_embedded"`
}

// MeasureResult holds the outcome of a single corridor measurement.
type MeasureResult struct {
	MeasuredAt     time.Time
	TargetUSDValue *float64
	SellAmount     float64
	ReceivedAmount *float64
	LossPct        *float64
	ReferenceRate  *float64
	ReferenceSrc   *string
	PathFound      bool
	RawResponse    json.RawMessage
	ErrorMsg       *string
}

// sourceAssetParams builds the source_asset_type/code/issuer query params for Horizon strict-send.
func sourceAssetParams(code, issuer string) url.Values {
	v := url.Values{}
	if code == "XLM" && issuer == "" {
		v.Set("source_asset_type", "native")
	} else {
		if len(code) > 4 {
			v.Set("source_asset_type", "credit_alphanum12")
		} else {
			v.Set("source_asset_type", "credit_alphanum4")
		}
		v.Set("source_asset_code", code)
		v.Set("source_asset_issuer", issuer)
	}
	return v
}

// destinationAssetStr encodes the destination asset in the CODE:ISSUER format
// required by /paths/strict-send's destination_assets parameter.
func destinationAssetStr(code, issuer string) string {
	if code == "XLM" && issuer == "" {
		return "native"
	}
	return code + ":" + issuer
}

// Measure calls Horizon strict-send pathfinding for a corridor, fetches the independent
// mid-market reference rate, computes loss_pct, and returns the result.
// It dynamically calculates the trade sell_amount using the corridor's target_usd_value
// scaled by the sell currency's USD exchange rate, ensuring real economic trade size.
func (h *HorizonClient) Measure(ctx context.Context, corridor store.Corridor, fallbackUSD float64) MeasureResult {
	targetUSD := corridor.TargetUSDValue
	if targetUSD <= 0 {
		targetUSD = fallbackUSD
	}
	if targetUSD <= 0 {
		targetUSD = 100.0
	}

	result := MeasureResult{
		MeasuredAt:     time.Now().UTC(),
		TargetUSDValue: &targetUSD,
	}

	fromCur := reference.AssetToCurrency(corridor.SellAssetCode)
	toCur := reference.AssetToCurrency(corridor.BuyAssetCode)

	// 1. Compute sell_amount dynamically from target_usd_value:
	// sell_amount = target_usd_value / (sell_currency -> USD rate)
	var sellToUSDRate float64 = 1.0
	if fromCur != "USD" && h.refFetcher != nil {
		r, _, err := h.refFetcher.FetchRate(ctx, fromCur, "USD")
		if err != nil {
			msg := fmt.Sprintf("fetching USD rate for sell asset %s: %v", fromCur, err)
			result.ErrorMsg = &msg
		} else if r > 0 {
			sellToUSDRate = r
		}
	}

	sellAmount := targetUSD / sellToUSDRate
	result.SellAmount = sellAmount

	// 2. Fetch independent mid-market reference rate between sell_asset and buy_asset.
	if h.refFetcher != nil {
		rate, src, err := h.refFetcher.FetchRate(ctx, fromCur, toCur)
		if err != nil {
			msg := fmt.Sprintf("fetching reference rate for %s/%s: %v", fromCur, toCur, err)
			result.ErrorMsg = &msg
		} else {
			result.ReferenceRate = &rate
			result.ReferenceSrc = &src
		}
	}

	// 3. Build and send Horizon query: paths/strict-send
	params := url.Values{}
	params.Set("source_amount", strconv.FormatFloat(sellAmount, 'f', 7, 64))

	for k, vs := range sourceAssetParams(corridor.SellAssetCode, corridor.SellAssetIssuer) {
		params[k] = vs
	}
	params.Set("destination_assets", destinationAssetStr(corridor.BuyAssetCode, corridor.BuyAssetIssuer))

	reqURL := fmt.Sprintf("%s/paths/strict-send?%s", h.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		msg := fmt.Sprintf("building request: %v", err)
		result.ErrorMsg = &msg
		return result
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		msg := fmt.Sprintf("http request: %v", err)
		result.ErrorMsg = &msg
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		msg := fmt.Sprintf("reading response body: %v", err)
		result.ErrorMsg = &msg
		return result
	}
	result.RawResponse = body

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("horizon returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		result.ErrorMsg = &msg
		return result
	}

	var parsed pathPaymentResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		msg := fmt.Sprintf("parsing horizon response: %v", err)
		result.ErrorMsg = &msg
		return result
	}

	if len(parsed.Embedded.Records) == 0 {
		// No path found — this is a valid measurement result (unusable state).
		return result
	}

	// Use the first (best) path record.
	best := parsed.Embedded.Records[0]
	received, err := strconv.ParseFloat(best.DestinationAmount, 64)
	if err != nil {
		msg := fmt.Sprintf("parsing destination_amount %q: %v", best.DestinationAmount, err)
		result.ErrorMsg = &msg
		return result
	}

	result.PathFound = true
	result.ReceivedAmount = &received

	// 4. Compute loss_pct against independent reference rate:
	// expected_received = sell_amount * reference_rate
	// loss_pct = (expected_received - received_amount) / expected_received * 100
	if result.ReferenceRate != nil && *result.ReferenceRate > 0 {
		expectedReceived := sellAmount * (*result.ReferenceRate)
		if expectedReceived > 0 {
			loss := (expectedReceived - received) / expectedReceived * 100
			result.LossPct = &loss
		}
	}

	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ScoreState converts a loss percentage to an IntegrityState based on deviation from reference rate.
//
// Threshold reasoning:
//   - loss_pct < degradedThreshold (default: 2.5%): Usable.
//     Normal payment corridor execution. Typical retail and anchor spreads on Stellar range from 0.1% to 2.0%.
//     Note: Large negative loss_pct on thin/illiquid assets (like NGNC) likely reflects multi-hop DEX
//     routing across intermediary pools (e.g. AQUA, native XLM, USDC) and shallow top-of-book effects
//     at the tested trade size (e.g. 100 units), rather than genuine favorable pricing. This is a
//     known measurement-methodology limitation on low-liquidity assets, not a settled explanation.
//   - degradedThreshold <= loss_pct < unusableThreshold (default: 2.5% to 5.0%): Degraded.
//     Pricing has widened beyond normal spreads; liquidity is shallow or slippage is elevated.
//     Payments will incur notable loss but can technically still route.
//   - loss_pct >= unusableThreshold (default: 5.0%): Unusable.
//     Loss exceeding 5% indicates severe illiquidity, de-pegging, or predatory spreads that make
//     the corridor economically unviable for real-world settlement.
//   - !pathFound or lossPct == nil: Unusable.
//     No payment path exists on the ledger, or reference rate pricing could not be determined.
func ScoreState(lossPct *float64, pathFound bool, degradedThreshold, unusableThreshold float64) store.IntegrityState {
	if !pathFound || lossPct == nil {
		return store.StateUnusable
	}
	l := *lossPct
	switch {
	case l >= unusableThreshold:
		return store.StateUnusable
	case l >= degradedThreshold:
		return store.StateDegraded
	default:
		return store.StateUsable
	}
}
