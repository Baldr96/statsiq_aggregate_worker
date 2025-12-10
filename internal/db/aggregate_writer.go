package db

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worker/internal/aggregate"
)

// AggregateWriter handles writing aggregate data to the database.
type AggregateWriter struct {
	pool *pgxpool.Pool
}

// NewAggregateWriter creates a new aggregate writer.
func NewAggregateWriter(pool *pgxpool.Pool) *AggregateWriter {
	return &AggregateWriter{pool: pool}
}

// WriteAll inserts all aggregate data within a single transaction.
// Uses advisory lock on match UUID to prevent concurrent processing.
// Purges existing aggregate data before insert for idempotency.
func (w *AggregateWriter) WriteAll(ctx context.Context, agg *aggregate.AggregateSet) error {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Advisory lock on match ID
	lockKey := advisoryLockKey(agg.MatchID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}

	// 2. Purge existing data (reverse FK order)
	if err := purgeAggregates(ctx, tx, agg.MatchID); err != nil {
		return fmt.Errorf("purge aggregates: %w", err)
	}

	// 3. Insert new data (FK order)
	if err := insertClutches(ctx, tx, agg.Clutches); err != nil {
		return fmt.Errorf("insert clutches: %w", err)
	}

	if err := insertRoundPlayerStats(ctx, tx, agg.RoundPlayerStats); err != nil {
		return fmt.Errorf("insert round player stats: %w", err)
	}

	if err := insertMatchPlayerStats(ctx, tx, agg.MatchPlayerStats); err != nil {
		return fmt.Errorf("insert match player stats: %w", err)
	}

	if err := insertTeamMatchStats(ctx, tx, agg.TeamMatchStats); err != nil {
		return fmt.Errorf("insert team match stats: %w", err)
	}

	if err := insertTeamMatchSideStats(ctx, tx, agg.TeamMatchSideStats); err != nil {
		return fmt.Errorf("insert team match side stats: %w", err)
	}

	return tx.Commit(ctx)
}

// advisoryLockKey generates a stable int64 key from a UUID for pg_advisory_lock.
func advisoryLockKey(id uuid.UUID) int64 {
	h := fnv.New64a()
	h.Write(id[:])
	return int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]))
}

// purgeAggregates deletes existing aggregate data for a match.
// Order: reverse of FK dependencies.
func purgeAggregates(ctx context.Context, tx pgx.Tx, matchID uuid.UUID) error {
	// 1. team_match_side_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM team_match_side_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge team_match_side_stats_agregate: %w", err)
	}

	// 2. team_match_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM team_match_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge team_match_stats_agregate: %w", err)
	}

	// 3. match_player_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM match_player_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge match_player_stats_agregate: %w", err)
	}

	// 4. round_player_stats_agregate (via JOIN rounds)
	if _, err := tx.Exec(ctx, `
		DELETE FROM round_player_stats_agregate rps
		USING rounds r
		WHERE rps.round_id = r.id AND r.match_id = $1
	`, matchID); err != nil {
		return fmt.Errorf("purge round_player_stats_agregate: %w", err)
	}

	// 5. clutches (via JOIN rounds)
	if _, err := tx.Exec(ctx, `
		DELETE FROM clutches c
		USING rounds r
		WHERE c.round_id = r.id AND r.match_id = $1
	`, matchID); err != nil {
		return fmt.Errorf("purge clutches: %w", err)
	}

	return nil
}

// insertClutches inserts clutch rows using COPY protocol.
func insertClutches(ctx context.Context, tx pgx.Tx, rows []aggregate.ClutchRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "round_id", "player_id", "side", "won", "is_clutcher",
		"situation", "type", "clutch_start_time_ms", "clutch_end_time_ms", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"clutches"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.RoundID, r.PlayerID, r.Side, r.Won, r.IsClutcher,
				r.Situation, r.Type, r.ClutchStartTimeMS, r.ClutchEndTimeMS, r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertRoundPlayerStats inserts round player stats using COPY protocol.
func insertRoundPlayerStats(ctx context.Context, tx pgx.Tx, rows []aggregate.RoundPlayerStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "round_id", "player_id", "loadout_id", "agent", "rating", "cs",
		"kills", "deaths", "assists", "headshot_percent",
		"headshot_kills", "bodyshot_kills", "legshot_kills",
		"headshot_hit", "bodyshot_hit", "legshot_hit",
		"damage_given", "damage_taken", "survived", "revived",
		"first_kill", "first_death", "suicides", "deaths_by_teammate", "teammates_killed", "killed_by_spike",
		"trade_kill", "traded_death",
		"clutch_id", "credits_spent", "credits_remaining", "is_overtime", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"round_player_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.RoundID, r.PlayerID, r.LoadoutID, r.Agent, r.Rating, r.CS,
				r.Kills, r.Deaths, r.Assists, r.HeadshotPercent,
				r.HeadshotKills, r.BodyshotKills, r.LegshotKills,
				r.HeadshotHit, r.BodyshotHit, r.LegshotHit,
				r.DamageGiven, r.DamageTaken, r.Survived, r.Revived,
				r.FirstKill, r.FirstDeath, r.Suicides, r.DeathsByTeammate, r.TeammatesKilled, r.KilledBySpike,
				r.TradeKill, r.TradedDeath,
				r.ClutchID, r.CreditsSpent, r.CreditsRemaining, r.IsOvertime, r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertMatchPlayerStats inserts match player stats using COPY protocol.
func insertMatchPlayerStats(ctx context.Context, tx pgx.Tx, rows []aggregate.MatchPlayerStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "player_id", "match_id", "rating", "acs", "kd", "kast", "adr",
		"kills", "deaths", "assists", "first_kills", "first_deaths",
		"trade_kills", "traded_deaths", "suicides", "teammates_killed", "deaths_by_spike", "chain_kills",
		"double_kills", "triple_kills", "quadra_kills", "penta_kills", "multi_kills",
		"clutches_played", "clutches_won",
		"v1_played", "v1_won", "v2_played", "v2_won",
		"v3_played", "v3_won", "v4_played", "v4_won", "v5_played", "v5_won",
		"headshot_percent", "headshot_kills", "bodyshot_kills", "legshot_kills",
		"headshot_hit", "bodyshot_hit", "legshot_hit",
		"damage_given", "damage_taken", "impact_score",
		"matches_played", "rounds_played", "win_rate", "is_overtime", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"match_player_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.PlayerID, r.MatchID, r.Rating, r.ACS, r.KD, r.KAST, r.ADR,
				r.Kills, r.Deaths, r.Assists, r.FirstKills, r.FirstDeaths,
				r.TradeKills, r.TradedDeaths, r.Suicides, r.TeammatesKilled, r.DeathsBySpike, r.ChainKills,
				r.DoubleKills, r.TripleKills, r.QuadraKills, r.PentaKills, r.MultiKills,
				r.ClutchesPlayed, r.ClutchesWon,
				r.V1Played, r.V1Won, r.V2Played, r.V2Won,
				r.V3Played, r.V3Won, r.V4Played, r.V4Won, r.V5Played, r.V5Won,
				r.HeadshotPercent, r.HeadshotKills, r.BodyshotKills, r.LegshotKills,
				r.HeadshotHit, r.BodyshotHit, r.LegshotHit,
				r.DamageGiven, r.DamageTaken, r.ImpactScore,
				r.MatchesPlayed, r.RoundsPlayed, r.WinRate, r.IsOvertime, r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertTeamMatchStats inserts team match stats using COPY protocol.
func insertTeamMatchStats(ctx context.Context, tx pgx.Tx, rows []aggregate.TeamMatchStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "team_id", "match_id", "match_type",
		"rounds_played", "rounds_won", "rounds_lost", "round_win_rate",
		"kd", "avg_kpr", "avg_dpr", "avg_apr", "avg_adr", "avg_acs", "damage_delta",
		"kills", "deaths", "first_kills", "first_deaths",
		"trade_kills", "traded_death", "suicides", "teammates_killed", "deaths_by_spike", "chain_kills",
		"double_kills", "triple_kills", "quadra_kills", "penta_kills", "multi_kills",
		"clutches_played", "clutches_won", "clutches_loss", "clutches_wr",
		"attack_rounds_win", "attack_rounds_played",
		"defense_rounds_wins", "defense_rounds_played",
		"match_won", "is_overtime", "rounds_overtime_won", "rounds_overtime_lost",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"team_match_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.TeamID, r.MatchID, r.MatchType,
				r.RoundsPlayed, r.RoundsWon, r.RoundsLost, r.RoundWinRate,
				r.KD, r.AvgKPR, r.AvgDPR, r.AvgAPR, r.AvgADR, r.AvgACS, r.DamageDelta,
				r.Kills, r.Deaths, r.FirstKills, r.FirstDeaths,
				r.TradeKills, r.TradedDeaths, r.Suicides, r.TeammatesKilled, r.DeathsBySpike, r.ChainKills,
				r.DoubleKills, r.TripleKills, r.QuadraKills, r.PentaKills, r.MultiKills,
				r.ClutchesPlayed, r.ClutchesWon, r.ClutchesLoss, r.ClutchesWR,
				r.AttackRoundsWin, r.AttackRoundsPlayed,
				r.DefenseRoundsWins, r.DefenseRoundsPlayed,
				r.MatchWon, r.IsOvertime, r.RoundsOvertimeWon, r.RoundsOvertimeLost,
				r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertTeamMatchSideStats inserts team match side stats using COPY protocol.
func insertTeamMatchSideStats(ctx context.Context, tx pgx.Tx, rows []aggregate.TeamMatchSideStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "team_id", "match_id", "match_type", "team_side", "side_outcome",
		"rounds_played", "rounds_won", "rounds_lost", "round_win_rate",
		"kd", "avg_kpr", "avg_dpr", "avg_apr", "avg_adr", "avg_acs", "damage_delta",
		"kills", "deaths", "first_kills", "first_deaths",
		"trade_kills", "traded_death", "suicides", "teammates_killed", "deaths_by_spike", "chain_kills",
		"double_kills", "triple_kills", "quadra_kills", "penta_kills", "multi_kills",
		"clutches_played", "clutches_won", "clutches_loss", "clutches_wr",
		"is_match_overtime", "rounds_overtime_won", "rounds_overtime_lost",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"team_match_side_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.TeamID, r.MatchID, r.MatchType, r.TeamSide, r.SideOutcome,
				r.RoundsPlayed, r.RoundsWon, r.RoundsLost, r.RoundWinRate,
				r.KD, r.AvgKPR, r.AvgDPR, r.AvgAPR, r.AvgADR, r.AvgACS, r.DamageDelta,
				r.Kills, r.Deaths, r.FirstKills, r.FirstDeaths,
				r.TradeKills, r.TradedDeaths, r.Suicides, r.TeammatesKilled, r.DeathsBySpike, r.ChainKills,
				r.DoubleKills, r.TripleKills, r.QuadraKills, r.PentaKills, r.MultiKills,
				r.ClutchesPlayed, r.ClutchesWon, r.ClutchesLoss, r.ClutchesWR,
				r.IsMatchOvertime, r.RoundsOvertimeWon, r.RoundsOvertimeLost,
				r.CreatedAt,
			}, nil
		}),
	)
	return err
}
