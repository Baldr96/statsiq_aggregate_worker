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

	// 1. Global advisory lock shared with canonical_worker to prevent deadlocks
	// between canonical writes and aggregate writes on related tables (rounds ↔ round_player_stats).
	// Lock key: shared constant "statsiq_write" = 0x7374617469717721
	const globalWriteLockKey int64 = 0x7374617469717721
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, globalWriteLockKey); err != nil {
		return fmt.Errorf("acquire global write lock: %w", err)
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

	if err := insertRoundTeamStats(ctx, tx, agg.RoundTeamStats); err != nil {
		return fmt.Errorf("insert round team stats: %w", err)
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

	if err := insertMatchPlayerDuels(ctx, tx, agg.MatchPlayerDuels); err != nil {
		return fmt.Errorf("insert match player duels: %w", err)
	}

	if err := insertMatchPlayerWeaponStats(ctx, tx, agg.MatchPlayerWeaponStats); err != nil {
		return fmt.Errorf("insert match player weapon stats: %w", err)
	}

	// Insert denormalized stats for CA support
	if err := insertPlayerClutchStats(ctx, tx, agg.PlayerClutchStats); err != nil {
		return fmt.Errorf("insert player clutch stats: %w", err)
	}

	if err := insertCompositionWeaponStats(ctx, tx, agg.CompositionWeaponStats); err != nil {
		return fmt.Errorf("insert composition weapon stats: %w", err)
	}

	if err := insertCompositionClutchStats(ctx, tx, agg.CompositionClutchStats); err != nil {
		return fmt.Errorf("insert composition clutch stats: %w", err)
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
	// 1. composition_clutch_stats_agregate (denormalized for CA)
	if _, err := tx.Exec(ctx, `DELETE FROM composition_clutch_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge composition_clutch_stats_agregate: %w", err)
	}

	// 2. composition_weapon_stats_agregate (denormalized for CA)
	if _, err := tx.Exec(ctx, `DELETE FROM composition_weapon_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge composition_weapon_stats_agregate: %w", err)
	}

	// 3. player_clutch_stats_agregate (denormalized for CA)
	if _, err := tx.Exec(ctx, `DELETE FROM player_clutch_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge player_clutch_stats_agregate: %w", err)
	}

	// 4. match_player_weapon_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM match_player_weapon_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge match_player_weapon_stats_agregate: %w", err)
	}

	// 5. match_player_duels_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM match_player_duels_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge match_player_duels_agregate: %w", err)
	}

	// 3. team_match_side_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM team_match_side_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge team_match_side_stats_agregate: %w", err)
	}

	// 4. team_match_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM team_match_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge team_match_stats_agregate: %w", err)
	}

	// 5. match_player_stats_agregate
	if _, err := tx.Exec(ctx, `DELETE FROM match_player_stats_agregate WHERE match_id = $1`, matchID); err != nil {
		return fmt.Errorf("purge match_player_stats_agregate: %w", err)
	}

	// 6. round_team_stats_agregate (via JOIN rounds)
	if _, err := tx.Exec(ctx, `
		DELETE FROM round_team_stats_agregate rts
		USING rounds r
		WHERE rts.round_id = r.id AND r.match_id = $1
	`, matchID); err != nil {
		return fmt.Errorf("purge round_team_stats_agregate: %w", err)
	}

	// 7. round_player_stats_agregate (via JOIN rounds)
	if _, err := tx.Exec(ctx, `
		DELETE FROM round_player_stats_agregate rps
		USING rounds r
		WHERE rps.round_id = r.id AND r.match_id = $1
	`, matchID); err != nil {
		return fmt.Errorf("purge round_player_stats_agregate: %w", err)
	}

	// 8. clutches (via JOIN rounds)
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
		"id", "round_id", "player_id", "match_date", "loadout_id", "agent", "rating", "cs",
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
				r.ID, r.RoundID, r.PlayerID, r.MatchDate, r.LoadoutID, r.Agent, r.Rating, r.CS,
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
		"id", "player_id", "match_id", "match_date", "rating", "acs", "kd", "kast", "adr",
		"kills", "deaths", "assists", "first_kills", "first_deaths",
		"trade_kills", "traded_deaths", "suicides", "teammates_killed", "deaths_by_spike", "chain_kills",
		"double_kills", "triple_kills", "quadra_kills", "penta_kills", "multi_kills",
		"clutches_played", "clutches_won",
		"v1_played", "v1_won", "v2_played", "v2_won",
		"v3_played", "v3_won", "v4_played", "v4_won", "v5_played", "v5_won",
		"headshot_percent", "headshot_kills", "bodyshot_kills", "legshot_kills",
		"headshot_hit", "bodyshot_hit", "legshot_hit",
		"damage_given", "damage_taken", "impact_score",
		"matches_played", "rounds_played", "rounds_win_rate_percent", "is_overtime",
		// Phase 5 columns for TimescaleDB CA support
		"rounds_won", "first_kills_traded", "first_deaths_traded", "mvp", "flawless_rounds", "match_won",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"match_player_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.PlayerID, r.MatchID, r.MatchDate, r.Rating, r.ACS, r.KD, r.KAST, r.ADR,
				r.Kills, r.Deaths, r.Assists, r.FirstKills, r.FirstDeaths,
				r.TradeKills, r.TradedDeaths, r.Suicides, r.TeammatesKilled, r.DeathsBySpike, r.ChainKills,
				r.DoubleKills, r.TripleKills, r.QuadraKills, r.PentaKills, r.MultiKills,
				r.ClutchesPlayed, r.ClutchesWon,
				r.V1Played, r.V1Won, r.V2Played, r.V2Won,
				r.V3Played, r.V3Won, r.V4Played, r.V4Won, r.V5Played, r.V5Won,
				r.HeadshotPercent, r.HeadshotKills, r.BodyshotKills, r.LegshotKills,
				r.HeadshotHit, r.BodyshotHit, r.LegshotHit,
				r.DamageGiven, r.DamageTaken, r.ImpactScore,
				r.MatchesPlayed, r.RoundsPlayed, r.RoundsWinRatePercent, r.IsOvertime,
				// Phase 5 columns
				r.RoundsWon, r.FirstKillsTraded, r.FirstDeathsTraded, r.MVP, r.FlawlessRounds, r.MatchWon,
				r.CreatedAt,
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
		"id", "team_id", "match_id", "match_date", "match_type",
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
				r.ID, r.TeamID, r.MatchID, r.MatchDate, r.MatchType,
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
		"id", "team_id", "match_id", "match_date", "match_type", "team_side", "side_outcome",
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
				r.ID, r.TeamID, r.MatchID, r.MatchDate, r.MatchType, r.TeamSide, r.SideOutcome,
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

// insertMatchPlayerDuels inserts match player duels using COPY protocol.
func insertMatchPlayerDuels(ctx context.Context, tx pgx.Tx, rows []aggregate.MatchPlayerDuelsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "match_id", "player_id", "opponent_id",
		"kills", "deaths", "first_kills", "first_deaths",
		"damage_given", "damage_taken", "headshot_kills",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"match_player_duels_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.MatchID, r.PlayerID, r.OpponentID,
				r.Kills, r.Deaths, r.FirstKills, r.FirstDeaths,
				r.DamageGiven, r.DamageTaken, r.HeadshotKills,
				r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertMatchPlayerWeaponStats inserts match player weapon stats using COPY protocol.
func insertMatchPlayerWeaponStats(ctx context.Context, tx pgx.Tx, rows []aggregate.MatchPlayerWeaponStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "match_id", "match_date", "player_id", "weapon_id",
		"weapon_name", "weapon_category",
		"kills", "deaths", "damage_given", "damage_taken",
		"first_kills", "headshot_kills", "bodyshot_kills", "legshot_kills",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"match_player_weapon_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.MatchID, r.MatchDate, r.PlayerID, r.WeaponID,
				r.WeaponName, r.WeaponCategory,
				r.Kills, r.Deaths, r.DamageGiven, r.DamageTaken,
				r.FirstKills, r.HeadshotKills, r.BodyshotKills, r.LegshotKills,
				r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertRoundTeamStats inserts round team stats using COPY protocol.
func insertRoundTeamStats(ctx context.Context, tx pgx.Tx, rows []aggregate.RoundTeamStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "round_id", "team_id", "match_date", "team_tag",
		"credits_spent", "credits_remaining", "buy_type",
		"kills", "deaths", "assists", "damage_given", "damage_taken",
		"first_kills", "first_deaths", "trade_kills", "traded_deaths",
		"side", "situation", "round_won", "is_overtime",
		"created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"round_team_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.RoundID, r.TeamID, r.MatchDate, r.TeamTag,
				r.CreditsSpent, r.CreditsRemaining, r.BuyType,
				r.Kills, r.Deaths, r.Assists, r.DamageGiven, r.DamageTaken,
				r.FirstKills, r.FirstDeaths, r.TradeKills, r.TradedDeaths,
				r.Side, r.Situation, r.RoundWon, r.IsOvertime,
				r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertPlayerClutchStats inserts player clutch stats (denormalized for CA) using COPY protocol.
func insertPlayerClutchStats(ctx context.Context, tx pgx.Tx, rows []aggregate.PlayerClutchStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "match_id", "match_date", "player_id", "clutch_type",
		"played", "won", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"player_clutch_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.MatchID, r.MatchDate, r.PlayerID, r.ClutchType,
				r.Played, r.Won, r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertCompositionWeaponStats inserts composition weapon stats (denormalized for CA) using COPY protocol.
func insertCompositionWeaponStats(ctx context.Context, tx pgx.Tx, rows []aggregate.CompositionWeaponStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "match_id", "match_date", "composition_hash", "weapon_category",
		"total_kills", "headshot_kills", "bodyshot_kills", "legshot_kills",
		"total_damage", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"composition_weapon_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.MatchID, r.MatchDate, r.CompositionHash, r.WeaponCategory,
				r.TotalKills, r.HeadshotKills, r.BodyshotKills, r.LegshotKills,
				r.TotalDamage, r.CreatedAt,
			}, nil
		}),
	)
	return err
}

// insertCompositionClutchStats inserts composition clutch stats (denormalized for CA) using COPY protocol.
func insertCompositionClutchStats(ctx context.Context, tx pgx.Tx, rows []aggregate.CompositionClutchStatsRow) error {
	if len(rows) == 0 {
		return nil
	}

	columns := []string{
		"id", "match_id", "match_date", "composition_hash", "clutch_type",
		"played", "won", "created_at",
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"composition_clutch_stats_agregate"},
		columns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			return []any{
				r.ID, r.MatchID, r.MatchDate, r.CompositionHash, r.ClutchType,
				r.Played, r.Won, r.CreatedAt,
			}, nil
		}),
	)
	return err
}
