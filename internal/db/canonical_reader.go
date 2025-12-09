package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worker/internal/aggregate"
)

// CanonicalReader provides read-only access to canonical tables.
type CanonicalReader struct {
	pool *pgxpool.Pool
}

// NewCanonicalReader creates a new canonical data reader.
func NewCanonicalReader(pool *pgxpool.Pool) *CanonicalReader {
	return &CanonicalReader{pool: pool}
}

// GetMatchData retrieves all canonical data for a match by its UUID.
func (r *CanonicalReader) GetMatchData(ctx context.Context, matchID uuid.UUID) (*aggregate.MatchData, error) {
	data := &aggregate.MatchData{
		MatchID: matchID,
		Players: make(map[uuid.UUID]aggregate.PlayerData),
	}

	// Get match info
	err := r.pool.QueryRow(ctx, `
		SELECT match_id, match_type, team_red_score, team_blue_score
		FROM matches
		WHERE id = $1
	`, matchID).Scan(&data.MatchKey, &data.MatchType, &data.TeamRedScore, &data.TeamBlueScore)
	if err != nil {
		return nil, fmt.Errorf("get match info: %w", err)
	}

	// Get rounds
	rounds, err := r.getRounds(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get rounds: %w", err)
	}
	data.Rounds = rounds

	// Get match players
	matchPlayers, err := r.getMatchPlayers(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get match players: %w", err)
	}
	data.MatchPlayers = matchPlayers

	// Get players
	players, err := r.getPlayers(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get players: %w", err)
	}
	data.Players = players

	// Get round events
	events, err := r.getRoundEvents(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get round events: %w", err)
	}
	data.RoundEvents = events

	// Get round player states
	states, err := r.getRoundPlayerStates(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get round player states: %w", err)
	}
	data.RoundPlayerStates = states

	// Get round player loadouts
	loadouts, err := r.getRoundPlayerLoadouts(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get round player loadouts: %w", err)
	}
	data.RoundPlayerLoadouts = loadouts

	return data, nil
}

// getRounds retrieves all rounds for a match.
func (r *CanonicalReader) getRounds(ctx context.Context, matchID uuid.UUID) ([]aggregate.RoundData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, round_number, winner, winning_team, win_method, spike_event, plant_time_ms
		FROM rounds
		WHERE match_id = $1
		ORDER BY round_number
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rounds []aggregate.RoundData
	for rows.Next() {
		var rd aggregate.RoundData
		if err := rows.Scan(&rd.ID, &rd.RoundNumber, &rd.WinnerTeamID, &rd.WinningTeam,
			&rd.WinMethod, &rd.SpikeEvent, &rd.PlantTimeMS); err != nil {
			return nil, err
		}
		rounds = append(rounds, rd)
	}
	return rounds, rows.Err()
}

// getMatchPlayers retrieves all match players with agent information.
func (r *CanonicalReader) getMatchPlayers(ctx context.Context, matchID uuid.UUID) ([]aggregate.MatchPlayerData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT mp.id, mp.match_id, mp.player_id, mp.team_id, mp.team_tag,
		       mp.agent_id, COALESCE(aa.name, 'Unknown') as agent_name
		FROM match_players mp
		LEFT JOIN asset_agents aa ON mp.agent_id = aa.id
		WHERE mp.match_id = $1
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []aggregate.MatchPlayerData
	for rows.Next() {
		var mp aggregate.MatchPlayerData
		if err := rows.Scan(&mp.ID, &mp.MatchID, &mp.PlayerID, &mp.TeamID, &mp.TeamTag,
			&mp.AgentID, &mp.AgentName); err != nil {
			return nil, err
		}
		players = append(players, mp)
	}
	return players, rows.Err()
}

// getPlayers retrieves player identity information.
func (r *CanonicalReader) getPlayers(ctx context.Context, matchID uuid.UUID) (map[uuid.UUID]aggregate.PlayerData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.puuid, p.name
		FROM players p
		JOIN match_players mp ON mp.player_id = p.id
		WHERE mp.match_id = $1
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	players := make(map[uuid.UUID]aggregate.PlayerData)
	for rows.Next() {
		var p aggregate.PlayerData
		if err := rows.Scan(&p.ID, &p.PUUID, &p.Name); err != nil {
			return nil, err
		}
		players[p.ID] = p
	}
	return players, rows.Err()
}

// getRoundEvents retrieves all round events (kills and damage).
func (r *CanonicalReader) getRoundEvents(ctx context.Context, matchID uuid.UUID) ([]aggregate.RoundEventData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, round_id, match_id, timestamp_ms, event_type,
		       player_id, victim_id, damage_given, headshot, bodyshot, legshot, weapon
		FROM round_events
		WHERE match_id = $1
		ORDER BY round_id, timestamp_ms
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []aggregate.RoundEventData
	for rows.Next() {
		var e aggregate.RoundEventData
		if err := rows.Scan(&e.ID, &e.RoundID, &e.MatchID, &e.TimestampMS, &e.EventType,
			&e.PlayerID, &e.VictimID, &e.DamageGiven, &e.Headshot, &e.Bodyshot, &e.Legshot, &e.Weapon); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// getRoundPlayerStates retrieves player states per round.
func (r *CanonicalReader) getRoundPlayerStates(ctx context.Context, matchID uuid.UUID) ([]aggregate.RoundPlayerStateData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rps.id, rps.round_id, rps.player_id, rps.score
		FROM round_player_state rps
		JOIN rounds r ON rps.round_id = r.id
		WHERE r.match_id = $1
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []aggregate.RoundPlayerStateData
	for rows.Next() {
		var s aggregate.RoundPlayerStateData
		if err := rows.Scan(&s.ID, &s.RoundID, &s.PlayerID, &s.Score); err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, rows.Err()
}

// getRoundPlayerLoadouts retrieves loadout and economy data.
func (r *CanonicalReader) getRoundPlayerLoadouts(ctx context.Context, matchID uuid.UUID) ([]aggregate.RoundPlayerLoadoutData, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rps.id as round_player_id, rpl.loadout_id, rpl.value, rpl.remaining, rpl.spent
		FROM round_player_state rps
		JOIN rounds r ON rps.round_id = r.id
		LEFT JOIN round_player_loadouts rpl ON rpl.round_player_id = rps.id
		WHERE r.match_id = $1
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loadouts []aggregate.RoundPlayerLoadoutData
	for rows.Next() {
		var l aggregate.RoundPlayerLoadoutData
		if err := rows.Scan(&l.RoundPlayerID, &l.LoadoutID, &l.Value, &l.Remaining, &l.Spent); err != nil {
			return nil, err
		}
		loadouts = append(loadouts, l)
	}
	return loadouts, rows.Err()
}

// MatchExists checks if a match exists in the database.
func (r *CanonicalReader) MatchExists(ctx context.Context, matchID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM matches WHERE id = $1)
	`, matchID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ErrNoRows is exposed for error checking.
var ErrNoRows = pgx.ErrNoRows
