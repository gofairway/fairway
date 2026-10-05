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

	"github.com/gofairway/fairway/internal/store"
)

// HorizonClient wraps Stellar Horizon strict-receive pathfinding.
type HorizonClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHorizonClient creates a HorizonClient pointing at the given Horizon base URL.
func NewHorizonClient(baseURL string) *HorizonClient {
	return &HorizonClient{
		baseURL: baseURL,
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
	SellAmount     float64
	ReceivedAmount *float64
	LossPct        *float64
	PathFound      bool
	RawResponse    json.RawMessage
	ErrorMsg       *string
}

// assetParam builds the asset_type / asset_code / asset_issuer query params for Horizon.
func assetParam(prefix, code, issuer string) url.Values {
	v := url.Values{}
	if code == "XLM" && issuer == "" {
		v.Set(prefix+"asset_type", "native")
	} else {
		v.Set(prefix+"asset_type", "credit_alphanum4")
		if len(code) > 4 {
			v.Set(prefix+"asset_type", "credit_alphanum12")
		}
		v.Set(prefix+"asset_code", code)
		v.Set(prefix+"asset_issuer", issuer)
	}
	return v
}

// Measure calls Horizon strict-send pathfinding for a corridor and returns the best path result.
func (h *HorizonClient) Measure(ctx context.Context, corridor store.Corridor, sellAmount float64) MeasureResult {
	result := MeasureResult{
		MeasuredAt: time.Now().UTC(),
		SellAmount: sellAmount,
	}

	// Build query: paths/strict-send
	// https://developers.stellar.org/api/horizon/resources/list-strict-send-payment-paths
	params := url.Values{}
	params.Set("source_amount", strconv.FormatFloat(sellAmount, 'f', 7, 64))

	// sell (source) asset
	for k, vs := range assetParam("source_", corridor.SellAssetCode, corridor.SellAssetIssuer) {
		params[k] = vs
	}
	// buy (destination) asset
	for k, vs := range assetParam("destination_", corridor.BuyAssetCode, corridor.BuyAssetIssuer) {
		params[k] = vs
	}

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
		// No path found — this is a valid (not error) measurement result.
		return result
	}

	// Use the first (best) record.
	best := parsed.Embedded.Records[0]
	received, err := strconv.ParseFloat(best.DestinationAmount, 64)
	if err != nil {
		msg := fmt.Sprintf("parsing destination_amount %q: %v", best.DestinationAmount, err)
		result.ErrorMsg = &msg
		return result
	}

	result.PathFound = true
	result.ReceivedAmount = &received

	// Loss % is relative to the sell amount expressed in the same unit as the
	// destination.  When a reference rate is available this is computed against
	// that.  Without one, we compute internal spread vs. expected parity
	// (i.e. assumes 1:1 as a neutral baseline — callers must apply a reference
	// rate if they want a meaningful loss figure).
	loss := (sellAmount - received) / sellAmount * 100
	result.LossPct = &loss

	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ScoreState converts a loss percentage to an IntegrityState.
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
