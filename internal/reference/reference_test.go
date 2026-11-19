package reference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssetToCurrency(t *testing.T) {
	tests := []struct {
		asset string
		want  string
	}{
		{"NGNC", "NGN"},
		{"USDC", "USD"},
		{"EURC", "EUR"},
		{"GHSC", "GHS"},
		{"KESC", "KES"},
		{"XLM", "XLM"},
		{"BRLC", "BRL"},
		{"USD", "USD"},
	}

	for _, tt := range tests {
		got := AssetToCurrency(tt.asset)
		if got != tt.want {
			t.Errorf("AssetToCurrency(%q) = %q, want %q", tt.asset, got, tt.want)
		}
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

type countingFetcher struct {
	calls atomic.Int64
	rate  float64
}

func (f *countingFetcher) FetchRate(context.Context, string, string) (float64, string, error) {
	f.calls.Add(1)
	return f.rate, "test", nil
}

func TestCachedFetcherReusesRateWithinTTL(t *testing.T) {
	underlying := &countingFetcher{rate: 1.25}
	fetcher := NewCachedFetcher(underlying, time.Minute)
	now := time.Now()
	fetcher.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		rate, _, err := fetcher.FetchRate(context.Background(), "NGN", "USD")
		if err != nil || rate != 1.25 {
			t.Fatalf("rate=%v err=%v", rate, err)
		}
	}
	if got := underlying.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}

	now = now.Add(2 * time.Minute)
	if _, _, err := fetcher.FetchRate(context.Background(), "NGN", "USD"); err != nil {
		t.Fatal(err)
	}
	if got := underlying.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 after expiry", got)
	}
}

func TestCachedFetcherZeroTTLDoesNotCache(t *testing.T) {
	underlying := &countingFetcher{rate: 1}
	fetcher := NewCachedFetcher(underlying, 0)
	for i := 0; i < 2; i++ {
		if _, _, err := fetcher.FetchRate(context.Background(), "NGN", "USD"); err != nil {
			t.Fatal(err)
		}
	}
	if got := underlying.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}
