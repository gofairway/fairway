package measure

import (
	"testing"

	"github.com/gofairway/fairway/internal/store"
)

func TestScoreState(t *testing.T) {
	degraded := 2.5
	unusable := 5.0

	tests := []struct {
		name      string
		lossPct   *float64
		pathFound bool
		want      store.IntegrityState
	}{
		{
			name:      "negative loss (better than benchmark)",
			lossPct:   ptr(-1.5),
			pathFound: true,
			want:      store.StateUsable,
		},
		{
			name:      "zero loss",
			lossPct:   ptr(0.0),
			pathFound: true,
			want:      store.StateUsable,
		},
		{
			name:      "low loss below degraded threshold",
			lossPct:   ptr(1.8),
			pathFound: true,
			want:      store.StateUsable,
		},
		{
			name:      "exactly at degraded threshold",
			lossPct:   ptr(2.5),
			pathFound: true,
			want:      store.StateDegraded,
		},
		{
			name:      "between degraded and unusable threshold",
			lossPct:   ptr(3.8),
			pathFound: true,
			want:      store.StateDegraded,
		},
		{
			name:      "exactly at unusable threshold",
			lossPct:   ptr(5.0),
			pathFound: true,
			want:      store.StateUnusable,
		},
		{
			name:      "above unusable threshold",
			lossPct:   ptr(12.5),
			pathFound: true,
			want:      store.StateUnusable,
		},
		{
			name:      "no path found",
			lossPct:   ptr(0.5),
			pathFound: false,
			want:      store.StateUnusable,
		},
		{
			name:      "nil loss percentage",
			lossPct:   nil,
			pathFound: true,
			want:      store.StateUnusable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreState(tt.lossPct, tt.pathFound, degraded, unusable)
			if got != tt.want {
				t.Errorf("ScoreState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptr(f float64) *float64 {
	return &f
}
