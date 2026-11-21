package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearEnv unsets every variable Load reads, so a test that expects defaults
// does not inherit one from the machine running it. `t.Setenv` cannot unset, so
// the cleanup is manual and ordered first: t.Cleanup runs LIFO, so the values
// are restored only after Load has run.
func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"DATABASE_URL", "HORIZON_URL", "SELL_AMOUNT", "MEASURE_INTERVAL",
		"DEGRADED_LOSS_PCT", "UNUSABLE_LOSS_PCT", "WEBHOOK_URL",
		"REFERENCE_RATE_URL", "CORRIDORS_FILE", "LISTEN_ADDR",
		"CORS_ALLOWED_ORIGINS", "RATE_LIMIT_ENABLED", "RATE_LIMIT_RPS",
		"RATE_LIMIT_BURST", "TRUST_PROXY",
	}
	original := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			original[key] = value
			os.Unsetenv(key)
		}
	}
	t.Cleanup(func() {
		for key, value := range original {
			os.Setenv(key, value)
		}
		for _, key := range keys {
			if _, ok := original[key]; !ok {
				os.Unsetenv(key)
			}
		}
	})
}

func writeCorridors(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corridors.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearEnv(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with no DATABASE_URL succeeded, want an error")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/fairway")
	path := writeCorridors(t, "corridors: []\n")
	t.Setenv("CORRIDORS_FILE", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	want := &AppConfig{
		DatabaseURL:        "postgres://localhost/fairway",
		HorizonURL:         "https://horizon.stellar.org",
		SellAmount:         100.0,
		MeasureInterval:    5 * time.Minute,
		DegradedLossPct:    2.5,
		UnusableLossPct:    5.0,
		WebhookURL:         "",
		ReferenceRateURL:   "https://api.frankfurter.dev/v2/rates",
		CorridorsFile:      path,
		ListenAddr:         ":8080",
		CORSAllowedOrigins: "",
		RateLimitEnabled:   true,
		RateLimitRPS:       10.0,
		RateLimitBurst:     30,
		TrustProxy:         false,
	}
	if cfg.DatabaseURL != want.DatabaseURL {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want.DatabaseURL)
	}
	if cfg.HorizonURL != want.HorizonURL {
		t.Errorf("HorizonURL = %q, want %q", cfg.HorizonURL, want.HorizonURL)
	}
	if cfg.SellAmount != want.SellAmount {
		t.Errorf("SellAmount = %v, want %v", cfg.SellAmount, want.SellAmount)
	}
	if cfg.MeasureInterval != want.MeasureInterval {
		t.Errorf("MeasureInterval = %v, want %v", cfg.MeasureInterval, want.MeasureInterval)
	}
	if cfg.DegradedLossPct != want.DegradedLossPct {
		t.Errorf("DegradedLossPct = %v, want %v", cfg.DegradedLossPct, want.DegradedLossPct)
	}
	if cfg.UnusableLossPct != want.UnusableLossPct {
		t.Errorf("UnusableLossPct = %v, want %v", cfg.UnusableLossPct, want.UnusableLossPct)
	}
	if cfg.WebhookURL != want.WebhookURL {
		t.Errorf("WebhookURL = %q, want %q", cfg.WebhookURL, want.WebhookURL)
	}
	if cfg.ReferenceRateURL != want.ReferenceRateURL {
		t.Errorf("ReferenceRateURL = %q, want %q", cfg.ReferenceRateURL, want.ReferenceRateURL)
	}
	if cfg.CorridorsFile != want.CorridorsFile {
		t.Errorf("CorridorsFile = %q, want %q", cfg.CorridorsFile, want.CorridorsFile)
	}
	if cfg.ListenAddr != want.ListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, want.ListenAddr)
	}
	if cfg.CORSAllowedOrigins != want.CORSAllowedOrigins {
		t.Errorf("CORSAllowedOrigins = %q, want %q", cfg.CORSAllowedOrigins, want.CORSAllowedOrigins)
	}
	if cfg.RateLimitEnabled != want.RateLimitEnabled {
		t.Errorf("RateLimitEnabled = %v, want %v", cfg.RateLimitEnabled, want.RateLimitEnabled)
	}
	if cfg.RateLimitRPS != want.RateLimitRPS {
		t.Errorf("RateLimitRPS = %v, want %v", cfg.RateLimitRPS, want.RateLimitRPS)
	}
	if cfg.RateLimitBurst != want.RateLimitBurst {
		t.Errorf("RateLimitBurst = %v, want %v", cfg.RateLimitBurst, want.RateLimitBurst)
	}
	if cfg.TrustProxy != want.TrustProxy {
		t.Errorf("TrustProxy = %v, want %v", cfg.TrustProxy, want.TrustProxy)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	clearEnv(t)
	path := writeCorridors(t, "corridors: []\n")
	t.Setenv("DATABASE_URL", "postgres://db/app")
	t.Setenv("HORIZON_URL", "https://horizon.example.com")
	t.Setenv("SELL_AMOUNT", "250.5")
	t.Setenv("MEASURE_INTERVAL", "90s")
	t.Setenv("DEGRADED_LOSS_PCT", "1.5")
	t.Setenv("UNUSABLE_LOSS_PCT", "9")
	t.Setenv("WEBHOOK_URL", "https://hooks.example.com/notify")
	t.Setenv("REFERENCE_RATE_URL", "https://rates.example.com")
	t.Setenv("CORRIDORS_FILE", path)
	t.Setenv("LISTEN_ADDR", "127.0.0.1:9090")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("RATE_LIMIT_RPS", "3.5")
	t.Setenv("RATE_LIMIT_BURST", "7")
	t.Setenv("TRUST_PROXY", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	if cfg.DatabaseURL != "postgres://db/app" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.HorizonURL != "https://horizon.example.com" {
		t.Errorf("HorizonURL = %q", cfg.HorizonURL)
	}
	if cfg.SellAmount != 250.5 {
		t.Errorf("SellAmount = %v, want 250.5", cfg.SellAmount)
	}
	if cfg.MeasureInterval != 90*time.Second {
		t.Errorf("MeasureInterval = %v, want 1m30s", cfg.MeasureInterval)
	}
	if cfg.DegradedLossPct != 1.5 {
		t.Errorf("DegradedLossPct = %v, want 1.5", cfg.DegradedLossPct)
	}
	if cfg.UnusableLossPct != 9 {
		t.Errorf("UnusableLossPct = %v, want 9", cfg.UnusableLossPct)
	}
	if cfg.WebhookURL != "https://hooks.example.com/notify" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.ReferenceRateURL != "https://rates.example.com" {
		t.Errorf("ReferenceRateURL = %q", cfg.ReferenceRateURL)
	}
	if cfg.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.CORSAllowedOrigins != "https://app.example.com" {
		t.Errorf("CORSAllowedOrigins = %q", cfg.CORSAllowedOrigins)
	}
	if cfg.RateLimitEnabled {
		t.Error("RateLimitEnabled = true, want false")
	}
	if cfg.RateLimitRPS != 3.5 {
		t.Errorf("RateLimitRPS = %v, want 3.5", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 7 {
		t.Errorf("RateLimitBurst = %v, want 7", cfg.RateLimitBurst)
	}
	if !cfg.TrustProxy {
		t.Error("TrustProxy = false, want true")
	}
}

// A malformed value is dropped back to the default rather than failing the
// start: an empty MEASURE_INTERVAL or a nonsense SELL_AMOUNT should not stop the
// server from booting.
func TestLoadFallsBackOnMalformedValues(t *testing.T) {
	clearEnv(t)
	path := writeCorridors(t, "corridors: []\n")
	t.Setenv("DATABASE_URL", "postgres://db/app")
	t.Setenv("CORRIDORS_FILE", path)
	t.Setenv("SELL_AMOUNT", "not-a-number")
	t.Setenv("MEASURE_INTERVAL", "five minutes")
	t.Setenv("RATE_LIMIT_BURST", "many")
	t.Setenv("RATE_LIMIT_ENABLED", "yes please")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.SellAmount != 100.0 {
		t.Errorf("SellAmount = %v, want the default 100", cfg.SellAmount)
	}
	if cfg.MeasureInterval != 5*time.Minute {
		t.Errorf("MeasureInterval = %v, want the default 5m", cfg.MeasureInterval)
	}
	if cfg.RateLimitBurst != 30 {
		t.Errorf("RateLimitBurst = %v, want the default 30", cfg.RateLimitBurst)
	}
	if !cfg.RateLimitEnabled {
		t.Error("RateLimitEnabled = false, want the default true")
	}
}

// LoadCorridors keeps only the enabled entries, so a corridor turned off in the
// file is never measured.
func TestLoadCorridorsFiltersDisabled(t *testing.T) {
	path := writeCorridors(t, `corridors:
  - name: "enabled corridor"
    sell_asset_code: "NGNC"
    enabled: true
  - name: "disabled corridor"
    sell_asset_code: "USDC"
    enabled: false
`)
	got, err := LoadCorridors(path)
	if err != nil {
		t.Fatalf("LoadCorridors(): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d corridors, want 1", len(got))
	}
	if got[0].Name != "enabled corridor" {
		t.Errorf("kept %q, want \"enabled corridor\"", got[0].Name)
	}
}

func TestLoadCorridorsErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := LoadCorridors(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
			t.Fatal("LoadCorridors() on a missing file succeeded, want an error")
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		path := writeCorridors(t, "corridors: [oops\n")
		if _, err := LoadCorridors(path); err == nil {
			t.Fatal("LoadCorridors() on malformed YAML succeeded, want an error")
		}
	})

	t.Run("no corridors key", func(t *testing.T) {
		got, err := LoadCorridors(writeCorridors(t, "something_else: true\n"))
		if err != nil {
			t.Fatalf("LoadCorridors(): %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d corridors, want 0", len(got))
		}
	})
}

func TestEnv(t *testing.T) {
	t.Setenv("FAIRWAY_TEST_ENV", "set")
	if got := env("FAIRWAY_TEST_ENV", "def"); got != "set" {
		t.Errorf("env() = %q, want %q", got, "set")
	}
	if got := env("FAIRWAY_TEST_ENV_UNSET", "def"); got != "def" {
		t.Errorf("env() = %q, want %q", got, "def")
	}
}

func TestEnvFloat(t *testing.T) {
	t.Setenv("FAIRWAY_TEST_FLOAT", "3.25")
	if got := envFloat("FAIRWAY_TEST_FLOAT", 1); got != 3.25 {
		t.Errorf("envFloat() = %v, want 3.25", got)
	}
	for _, value := range []string{"", "abc"} {
		t.Setenv("FAIRWAY_TEST_FLOAT", value)
		if got := envFloat("FAIRWAY_TEST_FLOAT", 7.5); got != 7.5 {
			t.Errorf("envFloat(%q) = %v, want the default 7.5", value, got)
		}
	}
	// Current behaviour, pinned rather than blessed: `fmt.Sscanf` stops at the first character it cannot
	// read, so a decimal comma parses as the leading integer instead of falling back. Switching to
	// `strconv.ParseFloat` (which would reject it) is a behaviour change, not a test.
	t.Setenv("FAIRWAY_TEST_FLOAT", "1,5")
	if got := envFloat("FAIRWAY_TEST_FLOAT", 7.5); got != 1 {
		t.Errorf("envFloat(%q) = %v, want the partial parse 1 (current behaviour)", "1,5", got)
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("FAIRWAY_TEST_DURATION", "10s")
	if got := envDuration("FAIRWAY_TEST_DURATION", time.Minute); got != 10*time.Second {
		t.Errorf("envDuration() = %v, want 10s", got)
	}
	for _, value := range []string{"", "10", "10s5"} {
		t.Setenv("FAIRWAY_TEST_DURATION", value)
		if got := envDuration("FAIRWAY_TEST_DURATION", time.Minute); got != time.Minute {
			t.Errorf("envDuration(%q) = %v, want the default 1m", value, got)
		}
	}
}

func TestEnvBool(t *testing.T) {
	for _, value := range []string{"true", "false", "1", "0", "T", "F"} {
		t.Setenv("FAIRWAY_TEST_BOOL", value)
		if got := envBool("FAIRWAY_TEST_BOOL", true); got != (value == "true" || value == "1" || value == "T") {
			t.Errorf("envBool(%q) = %v", value, got)
		}
	}
	for _, value := range []string{"", "maybe", "2"} {
		t.Setenv("FAIRWAY_TEST_BOOL", value)
		if got := envBool("FAIRWAY_TEST_BOOL", true); !got {
			t.Errorf("envBool(%q) = false, want the default true", value)
		}
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("FAIRWAY_TEST_INT", "42")
	if got := envInt("FAIRWAY_TEST_INT", 1); got != 42 {
		t.Errorf("envInt() = %v, want 42", got)
	}
	for _, value := range []string{"", "42.5", "many", "0x10"} {
		t.Setenv("FAIRWAY_TEST_INT", value)
		if got := envInt("FAIRWAY_TEST_INT", 9); got != 9 {
			t.Errorf("envInt(%q) = %v, want the default 9", value, got)
		}
	}
}
