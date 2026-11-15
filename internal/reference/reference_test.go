package reference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetToCurrency(t *testing.T) {
	tests := []struct {
		name  string
		asset string
		want  string
	}{
		{"explicit custodial", "NGNC", "NGN"},
		{"explicit custodial usdc", "USDC", "USD"},
		{"explicit custodial eurc", "EURC", "EUR"},
		{"explicit custodial ghsc", "GHSC", "GHS"},
		{"explicit custodial kesc", "KESC", "KES"},
		{"native asset", "XLM", "XLM"},
		{"derived from trailing c", "BRLC", "BRL"},
		{"fiat code passes through", "USD", "USD"},
		{"fiat code of 3 letters", "NGN", "NGN"},

		{"stablecoin usdt", "USDT", "USD"},
		{"stablecoin eurt", "EURT", "EUR"},
		{"audd", "AUDD", "AUD"},
		{"xsgd", "XSGD", "SGD"},
		{"idrt", "IDRT", "IDR"},
		{"arst", "ARST", "ARS"},
		{"brla", "BRLA", "BRL"},

		{"lower case", "usdc", "USD"},
		{"mixed case", "UsDt", "USD"},
		{"leading and trailing space", "  NGNC	", "NGN"},
		{"unknown four letters", "ABCD", "ABCD"},
		{"unknown three letters", "ZZZ", "ZZZ"},
		{"empty", "", ""},
		{"only spaces", "   ", ""},
		{"longer than four", "USDCX", "USDCX"},
		{"five letters ending in c", "USDCX", "USDCX"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssetToCurrency(tt.asset); got != tt.want {
				t.Errorf("AssetToCurrency(%q) = %q, want %q", tt.asset, got, tt.want)
			}
		})
	}
}

func TestFrankfurterFetcher(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := r.URL.Query().Get("base")
		quotes := r.URL.Query().Get("quotes")
		if base == "ngn" && quotes == "usd" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"date":"2026-10-05","base":"NGN","quote":"USD","rate":0.00075}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	fetcher := NewFrankfurterFetcher(ts.URL)
	rate, src, err := fetcher.FetchRate(context.Background(), "NGN", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 0.00075 {
		t.Errorf("expected rate 0.00075, got %f", rate)
	}
	if src != "frankfurter.app" {
		t.Errorf("expected src 'frankfurter.app', got %s", src)
	}
}
