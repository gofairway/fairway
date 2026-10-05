// Package seed upserts corridor configs from YAML into the database.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofairway/fairway/internal/config"
	"github.com/gofairway/fairway/internal/store"
)

// Corridors upserts each enabled corridor from the config into the database.
// Returns the number of corridors upserted.
func Corridors(ctx context.Context, st *store.Store, corridors []config.CorridorConfig, logger *slog.Logger) (int, error) {
	count := 0
	for _, cc := range corridors {
		var verDate *time.Time
		if cc.VerificationDate != "" {
			t, err := time.Parse("2006-01-02", cc.VerificationDate)
			if err != nil {
				return count, fmt.Errorf("parsing verification_date for %q: %w", cc.Name, err)
			}
			verDate = &t
		}

		row := store.Corridor{
			Name:             cc.Name,
			SellAssetCode:    cc.SellAssetCode,
			SellAssetIssuer:  cc.SellAssetIssuer,
			BuyAssetCode:     cc.BuyAssetCode,
			BuyAssetIssuer:   cc.BuyAssetIssuer,
			Domain:           cc.Domain,
			AnchorMetadata:   cc.AnchorMetadata,
			VerificationDate: verDate,
			VerifiedStatus:   cc.VerifiedStatus,
			Enabled:          cc.Enabled,
		}

		id, err := st.UpsertCorridor(ctx, row)
		if err != nil {
			return count, fmt.Errorf("upserting corridor %q: %w", cc.Name, err)
		}
		logger.Info("seeded corridor", "name", cc.Name, "id", id)
		count++
	}
	return count, nil
}
