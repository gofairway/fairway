package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCorridors(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corridors.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setOptionalEnvEmpty(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HORIZON_URL", "SELL_AMOUNT", "MEASURE_INTERVAL", "DEGRADED_LOSS_PCT",
		"UNUSABLE_LOSS_PCT", "WEBHOOK_URL", "REFERENCE_RATE_URL", "LISTEN_ADDR",
		"CORS_ALLOWED_ORIGINS", "RATE_LIMIT_ENABLED", "RATE_LIMIT_RPS",
		"RATE_LIMIT_BURST", "TRUST_PROXY",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	corridorsPath := writeCorridors(t, "corridors: []\n")
	t.Setenv("CORRIDORS_FILE", corridorsPath)
	setOptionalEnvEmpty(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HorizonURL != "https://horizon.stellar.org" ||
		cfg.SellAmount != 100 ||
		cfg.MeasureInterval != 5*time.Minute ||
		cfg.DegradedLossPct != 2.5 ||
		cfg.UnusableLossPct != 5 ||
		cfg.ReferenceRateURL != "https://api.frankfurter.dev/v2/rates" ||
		cfg.CorridorsFile != corridorsPath ||
		cfg.ListenAddr != ":8080" ||
		!cfg.RateLimitEnabled ||
		cfg.RateLimitRPS != 10 ||
		cfg.RateLimitBurst != 30 ||
		cfg.TrustProxy {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("CORRIDORS_FILE", writeCorridors(t, "corridors: []\n"))
	t.Setenv("HORIZON_URL", "https://horizon.example")
	t.Setenv("SELL_AMOUNT", "42.5")
	t.Setenv("MEASURE_INTERVAL", "90s")
	t.Setenv("DEGRADED_LOSS_PCT", "1.5")
	t.Setenv("UNUSABLE_LOSS_PCT", "3.5")
	t.Setenv("WEBHOOK_URL", "https://hooks.example/corridor")
	t.Setenv("REFERENCE_RATE_URL", "https://rates.example")
	t.Setenv("LISTEN_ADDR", "127.0.0.1:9090")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("RATE_LIMIT_RPS", "2.5")
	t.Setenv("RATE_LIMIT_BURST", "7")
	t.Setenv("TRUST_PROXY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HorizonURL != "https://horizon.example" ||
		cfg.SellAmount != 42.5 ||
		cfg.MeasureInterval != 90*time.Second ||
		cfg.DegradedLossPct != 1.5 ||
		cfg.UnusableLossPct != 3.5 ||
		cfg.WebhookURL != "https://hooks.example/corridor" ||
		cfg.ReferenceRateURL != "https://rates.example" ||
		cfg.ListenAddr != "127.0.0.1:9090" ||
		cfg.CORSAllowedOrigins != "https://app.example" ||
		cfg.RateLimitEnabled ||
		cfg.RateLimitRPS != 2.5 ||
		cfg.RateLimitBurst != 7 ||
		!cfg.TrustProxy {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestLoadInvalidEnvFallsBackToDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("CORRIDORS_FILE", writeCorridors(t, "corridors: []\n"))
	t.Setenv("SELL_AMOUNT", "not-a-number")
	t.Setenv("MEASURE_INTERVAL", "not-a-duration")
	t.Setenv("RATE_LIMIT_ENABLED", "not-a-bool")
	t.Setenv("RATE_LIMIT_BURST", "not-an-int")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SellAmount != 100 || cfg.MeasureInterval != 5*time.Minute ||
		!cfg.RateLimitEnabled || cfg.RateLimitBurst != 30 {
		t.Fatalf("invalid values should fall back: %+v", cfg)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CORRIDORS_FILE", writeCorridors(t, "corridors: []\n"))
	if _, err := Load(); err == nil || err.Error() != "DATABASE_URL is required" {
		t.Fatalf("got %v, want DATABASE_URL error", err)
	}
}

func TestLoadMissingCorridorsFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("CORRIDORS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	if _, err := Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want file-not-found", err)
	}
}

func TestLoadCorridorsFiltersDisabled(t *testing.T) {
	path := writeCorridors(t, `
corridors:
  - name: enabled
    enabled: true
    sell_asset_code: USDC
    target_usd_value: 250
  - name: disabled
    enabled: false
  - name: implicit-disabled
`)
	corridors, err := LoadCorridors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(corridors) != 1 || corridors[0].Name != "enabled" ||
		corridors[0].SellAssetCode != "USDC" || corridors[0].TargetUSDValue != 250 {
		t.Fatalf("unexpected corridors: %+v", corridors)
	}
}

func TestLoadCorridorsInvalidYAML(t *testing.T) {
	path := writeCorridors(t, "corridors: [")
	_, err := LoadCorridors(path)
	if err == nil || !strings.Contains(err.Error(), "parsing "+path+":") {
		t.Fatalf("got %v, want YAML parse error for %s", err, path)
	}
}
