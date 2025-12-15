package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"worker/internal/logging"
)

// CARefresher handles refreshing TimescaleDB Continuous Aggregates after match processing.
type CARefresher struct {
	pool *pgxpool.Pool
}

// NewCARefresher creates a new CA refresher instance.
func NewCARefresher(pool *pgxpool.Pool) *CARefresher {
	return &CARefresher{pool: pool}
}

// continuousAggregates lists all CAs that need to be refreshed after match processing.
// Ordered by dependency: base CAs first, then derived CAs.
var continuousAggregates = []string{
	// Player-level CAs
	"ca_player_daily_stats",
	"ca_player_side_daily_stats",
	"ca_player_map_stats",
	"ca_player_agent_stats",
	"ca_player_map_side_stats",
	"ca_player_economy_daily_stats",
	"ca_player_weapon_daily_stats",
	"ca_player_clutch_stats",
	"ca_player_situation_stats",
	"ca_player_pistol_stats",
	"ca_player_round_outcome_stats",

	// Composition-level CAs
	"ca_composition_daily_stats",
	"ca_composition_map_daily_stats",
	"ca_composition_economy_stats",
	"ca_composition_weapon_stats",
	"ca_composition_clutch_stats",
	"ca_composition_situation_stats",

	// Team-level CAs
	"ca_team_daily_stats",
	"ca_team_player_daily_stats",
	"ca_team_map_daily_stats",
	"ca_team_agent_daily_stats",
	"ca_team_outcome_daily_stats",
	"ca_team_player_duels_daily_stats",
}

// RefreshForMatchDate refreshes all Continuous Aggregates for a specific match date.
// Uses a window of [matchDate - 1 day, matchDate + 1 day] to ensure proper bucket coverage.
func (r *CARefresher) RefreshForMatchDate(ctx context.Context, matchDate time.Time) error {
	logger := logging.Logger()

	// Calculate refresh window: matchDate ± 1 day
	// This ensures the time bucket containing the match is fully refreshed
	windowStart := matchDate.Truncate(24 * time.Hour).Add(-24 * time.Hour)
	windowEnd := matchDate.Truncate(24 * time.Hour).Add(48 * time.Hour)

	logger.Infof("refreshing %d CAs for date window [%s, %s]",
		len(continuousAggregates),
		windowStart.Format("2006-01-02"),
		windowEnd.Format("2006-01-02"))

	startTime := time.Now()
	refreshed := 0

	for _, ca := range continuousAggregates {
		if err := r.refreshCA(ctx, ca, windowStart, windowEnd); err != nil {
			// Log error but continue with other CAs
			logger.Warnf("failed to refresh CA %s: %v", ca, err)
			continue
		}
		refreshed++
	}

	elapsed := time.Since(startTime)
	logger.Infof("CA refresh completed: %d/%d succeeded in %v", refreshed, len(continuousAggregates), elapsed)

	if refreshed == 0 {
		return fmt.Errorf("all CA refreshes failed")
	}

	return nil
}

// RefreshAll refreshes all Continuous Aggregates with NULL bounds (full refresh).
// Use this for initial data load or historical backfill.
func (r *CARefresher) RefreshAll(ctx context.Context) error {
	logger := logging.Logger()

	logger.Infof("performing full refresh of %d CAs", len(continuousAggregates))

	startTime := time.Now()
	refreshed := 0

	for _, ca := range continuousAggregates {
		if err := r.refreshCAFull(ctx, ca); err != nil {
			logger.Warnf("failed to refresh CA %s: %v", ca, err)
			continue
		}
		refreshed++
	}

	elapsed := time.Since(startTime)
	logger.Infof("full CA refresh completed: %d/%d succeeded in %v", refreshed, len(continuousAggregates), elapsed)

	if refreshed == 0 {
		return fmt.Errorf("all CA refreshes failed")
	}

	return nil
}

// refreshCA refreshes a single CA for a specific time window.
func (r *CARefresher) refreshCA(ctx context.Context, caName string, start, end time.Time) error {
	query := fmt.Sprintf(
		"CALL refresh_continuous_aggregate('%s', '%s'::timestamptz, '%s'::timestamptz)",
		caName,
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)

	_, err := r.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("refresh %s: %w", caName, err)
	}

	return nil
}

// refreshCAFull refreshes a single CA with NULL bounds (full refresh).
func (r *CARefresher) refreshCAFull(ctx context.Context, caName string) error {
	query := fmt.Sprintf("CALL refresh_continuous_aggregate('%s', NULL, NULL)", caName)

	_, err := r.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("full refresh %s: %w", caName, err)
	}

	return nil
}
