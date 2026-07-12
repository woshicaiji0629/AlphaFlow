package app

import (
	"log/slog"
	"testing"

	"alphaflow/go-service/market-data/internal/collector"
)

func TestCollectorStatsLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		previous collector.Stats
		current  collector.Stats
		want     slog.Level
	}{
		{
			name:    "healthy",
			current: collector.Stats{QueueLen: 79, QueueCap: 100},
			want:    slog.LevelInfo,
		},
		{
			name:    "queue at threshold",
			current: collector.Stats{QueueLen: 80, QueueCap: 100},
			want:    slog.LevelWarn,
		},
		{
			name:     "new process error",
			previous: collector.Stats{ProcessEventErrors: 2},
			current:  collector.Stats{ProcessEventErrors: 3},
			want:     slog.LevelWarn,
		},
		{
			name:     "unchanged cumulative errors",
			previous: collector.Stats{ProcessEventErrors: 3, DroppedLatestEvents: 2, KlineGapRequestErrors: 1},
			current:  collector.Stats{ProcessEventErrors: 3, DroppedLatestEvents: 2, KlineGapRequestErrors: 1},
			want:     slog.LevelInfo,
		},
		{
			name:     "new dropped event",
			previous: collector.Stats{DroppedLatestEvents: 1},
			current:  collector.Stats{DroppedLatestEvents: 2},
			want:     slog.LevelWarn,
		},
		{
			name:     "new gap request error",
			previous: collector.Stats{KlineGapRequestErrors: 1},
			current:  collector.Stats{KlineGapRequestErrors: 2},
			want:     slog.LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectorStatsLogLevel(tt.previous, tt.current); got != tt.want {
				t.Fatalf("collectorStatsLogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}
