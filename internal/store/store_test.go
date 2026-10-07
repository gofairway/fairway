package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEffectiveVerifiedStatus(t *testing.T) {
	tests := []struct {
		name string
		sell string
		buy  string
		want string
	}{
		{"both live", "live", "live", "live"},
		{"live and pending", "live", "pending", "pending"},
		{"pending and live", "pending", "live", "pending"},
		{"live and unknown", "live", "unknown", "unknown"},
		{"unknown and live", "unknown", "live", "unknown"},
		{"live and unverifiable", "live", "unverifiable", "unverifiable"},
		{"unverifiable and live", "unverifiable", "live", "unverifiable"},
		{"pending and unverifiable", "pending", "unverifiable", "unverifiable"},
		{"both unverifiable", "unverifiable", "unverifiable", "unverifiable"},
		{"unrecognized fallback to unknown", "custom_status", "live", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Corridor{
				SellVerifiedStatus: tt.sell,
				BuyVerifiedStatus:  tt.buy,
			}
			if got := c.EffectiveVerifiedStatus(); got != tt.want {
				t.Errorf("EffectiveVerifiedStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func getTestStore(t *testing.T) *Store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		for _, u := range []string{
			"postgres://fairway:fairway@localhost:5433/fairway?sslmode=disable",
			"postgres://fairway:fairway@localhost:5432/fairway?sslmode=disable",
		} {
			s, err := New(u)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err = s.Ping(ctx)
				cancel()
				if err == nil {
					return s
				}
				s.Close()
			}
		}
		t.Skip("PostgreSQL database not available; skipping database integration test")
		return nil
	}

	s, err := New(dbURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		s.Close()
		t.Skipf("database at %s not reachable: %v", dbURL, err)
		return nil
	}
	return s
}

func TestStore_CorridorCRUD(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	ctx := context.Background()
	testCorridorName := "TEST_CORRIDOR_" + time.Now().Format("20060102150405.000")
	now := time.Now().Truncate(time.Second)

	c := Corridor{
		Name:               testCorridorName,
		SellAssetCode:      "TESTA",
		SellAssetIssuer:    "GA1111111111111111111111111111111111111111111111111111111111",
		BuyAssetCode:       "TESTB",
		BuyAssetIssuer:     "GB2222222222222222222222222222222222222222222222222222222222",
		SellDomain:         "testa.org",
		SellAnchorMetadata: "full",
		SellVerifiedStatus: "live",
		BuyDomain:          "testb.org",
		BuyAnchorMetadata:  "none",
		BuyVerifiedStatus:  "unverifiable",
		VerificationDate:   &now,
		TargetUSDValue:     150.0,
		Enabled:            true,
	}

	// 1. Insert corridor
	id, err := st.UpsertCorridor(ctx, c)
	if err != nil {
		t.Fatalf("UpsertCorridor insert error: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	// 2. Get corridor
	fetched, err := st.GetCorridor(ctx, id)
	if err != nil {
		t.Fatalf("GetCorridor error: %v", err)
	}
	if fetched == nil {
		t.Fatalf("corridor with ID %d not found", id)
	}
	if fetched.Name != c.Name {
		t.Errorf("expected name %q, got %q", c.Name, fetched.Name)
	}
	if fetched.TargetUSDValue != 150.0 {
		t.Errorf("expected TargetUSDValue 150.0, got %f", fetched.TargetUSDValue)
	}
	if fetched.SellVerifiedStatus != "live" || fetched.BuyVerifiedStatus != "unverifiable" {
		t.Errorf("unexpected per-leg verification statuses: %s / %s", fetched.SellVerifiedStatus, fetched.BuyVerifiedStatus)
	}

	// 3. Update corridor on conflict
	c.TargetUSDValue = 200.0
	c.SellDomain = "updated.org"
	updatedID, err := st.UpsertCorridor(ctx, c)
	if err != nil {
		t.Fatalf("UpsertCorridor update error: %v", err)
	}
	if updatedID != id {
		t.Errorf("expected updated ID %d to equal original ID %d", updatedID, id)
	}

	fetchedUpdated, err := st.GetCorridor(ctx, id)
	if err != nil {
		t.Fatalf("GetCorridor after update error: %v", err)
	}
	if fetchedUpdated.TargetUSDValue != 200.0 {
		t.Errorf("expected updated TargetUSDValue 200.0, got %f", fetchedUpdated.TargetUSDValue)
	}
	if fetchedUpdated.SellDomain != "updated.org" {
		t.Errorf("expected updated SellDomain 'updated.org', got %q", fetchedUpdated.SellDomain)
	}

	// 4. List corridors
	corridors, err := st.ListCorridors(ctx)
	if err != nil {
		t.Fatalf("ListCorridors error: %v", err)
	}
	var found bool
	for _, item := range corridors {
		if item.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected corridor ID %d to be in ListCorridors", id)
	}
}

func TestStore_MeasurementCRUD(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	ctx := context.Background()
	testCorridorName := "TEST_MEASURE_" + time.Now().Format("20060102150405.000")
	c := Corridor{
		Name:            testCorridorName,
		SellAssetCode:   "USDC",
		SellAssetIssuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		BuyAssetCode:    "EURC",
		BuyAssetIssuer:  "GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2",
		TargetUSDValue:  100.0,
		Enabled:         true,
	}
	corridorID, err := st.UpsertCorridor(ctx, c)
	if err != nil {
		t.Fatalf("failed to insert corridor for measurement test: %v", err)
	}

	targetUSD := 100.0
	received := 89.5
	loss := 1.25
	rate := 0.895
	src := "frankfurter.app"

	m1 := Measurement{
		CorridorID:     corridorID,
		MeasuredAt:     time.Now().Add(-1 * time.Minute),
		TargetUSDValue: &targetUSD,
		SellAmount:     100.0,
		ReceivedAmount: &received,
		LossPct:        &loss,
		ReferenceRate:  &rate,
		ReferenceSrc:   &src,
		IntegrityState: StateUsable,
		PathFound:      true,
	}

	id1, err := st.InsertMeasurement(ctx, m1)
	if err != nil {
		t.Fatalf("InsertMeasurement m1 error: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive measurement ID, got %d", id1)
	}

	latest, err := st.LatestMeasurement(ctx, corridorID)
	if err != nil {
		t.Fatalf("LatestMeasurement error: %v", err)
	}
	if latest == nil || latest.ID != id1 {
		t.Fatalf("expected latest measurement ID %d, got %+v", id1, latest)
	}
	if latest.IntegrityState != StateUsable {
		t.Errorf("expected StateUsable, got %v", latest.IntegrityState)
	}
	if latest.LossPct == nil || *latest.LossPct != loss {
		t.Errorf("expected loss %f, got %v", loss, latest.LossPct)
	}

	// Insert second newer measurement
	loss2 := 6.5
	m2 := Measurement{
		CorridorID:     corridorID,
		MeasuredAt:     time.Now(),
		TargetUSDValue: &targetUSD,
		SellAmount:     100.0,
		ReceivedAmount: &received,
		LossPct:        &loss2,
		ReferenceRate:  &rate,
		ReferenceSrc:   &src,
		IntegrityState: StateUnusable,
		PathFound:      true,
	}

	id2, err := st.InsertMeasurement(ctx, m2)
	if err != nil {
		t.Fatalf("InsertMeasurement m2 error: %v", err)
	}

	latest2, err := st.LatestMeasurement(ctx, corridorID)
	if err != nil {
		t.Fatalf("LatestMeasurement m2 error: %v", err)
	}
	if latest2 == nil || latest2.ID != id2 {
		t.Fatalf("expected latest measurement ID %d, got %+v", id2, latest2)
	}
	if latest2.IntegrityState != StateUnusable {
		t.Errorf("expected StateUnusable, got %v", latest2.IntegrityState)
	}

	// List measurements
	list, err := st.ListMeasurements(ctx, corridorID, 10, 0)
	if err != nil {
		t.Fatalf("ListMeasurements error: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2 measurements in list, got %d", len(list))
	}
	// Ordered by measured_at DESC
	if list[0].ID != id2 {
		t.Errorf("expected first item in list to be newest (id %d), got %d", id2, list[0].ID)
	}
}

func TestStore_StateChangeAndWebhooks(t *testing.T) {
	st := getTestStore(t)
	defer st.Close()

	ctx := context.Background()
	testCorridorName := "TEST_EVENTS_" + time.Now().Format("20060102150405.000")
	corridorID, err := st.UpsertCorridor(ctx, Corridor{
		Name:            testCorridorName,
		SellAssetCode:   "USDC",
		SellAssetIssuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		BuyAssetCode:    "EURC",
		BuyAssetIssuer:  "GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2",
		TargetUSDValue:  100.0,
		Enabled:         true,
	})
	if err != nil {
		t.Fatalf("insert corridor error: %v", err)
	}

	event := StateChangeEvent{
		CorridorID:  corridorID,
		FromState:   StateUsable,
		ToState:     StateDegraded,
		OccurredAt:  time.Now(),
		WebhookSent: false,
	}

	eventID, err := st.InsertStateChange(ctx, event)
	if err != nil {
		t.Fatalf("InsertStateChange error: %v", err)
	}

	pending, err := st.PendingWebhooks(ctx)
	if err != nil {
		t.Fatalf("PendingWebhooks error: %v", err)
	}
	var found bool
	for _, p := range pending {
		if p.ID == eventID {
			found = true
			if p.FromState != StateUsable || p.ToState != StateDegraded {
				t.Errorf("unexpected states: from=%s, to=%s", p.FromState, p.ToState)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected event %d to be in PendingWebhooks", eventID)
	}

	// Mark webhook sent
	if err := st.MarkWebhookSent(ctx, eventID); err != nil {
		t.Fatalf("MarkWebhookSent error: %v", err)
	}

	// Verify it is no longer pending
	pendingAfter, err := st.PendingWebhooks(ctx)
	if err != nil {
		t.Fatalf("PendingWebhooks after mark error: %v", err)
	}
	for _, p := range pendingAfter {
		if p.ID == eventID {
			t.Errorf("event %d should not be pending after MarkWebhookSent", eventID)
		}
	}
}
