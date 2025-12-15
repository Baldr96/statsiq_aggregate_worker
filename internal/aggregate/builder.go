package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// MatchData holds all canonical data needed for aggregate computation.
// This is defined in the aggregate package to avoid import cycles.
type MatchData struct {
	MatchID             uuid.UUID
	MatchKey            string
	MatchDate           time.Time // Date of the match for TimescaleDB hypertable partitioning
	MatchType           *string
	TeamRedScore        int16
	TeamBlueScore       int16
	Rounds              []RoundData
	MatchPlayers        []MatchPlayerData
	Players             map[uuid.UUID]PlayerData
	RoundEvents         []RoundEventData
	RoundPlayerStates   []RoundPlayerStateData
	RoundPlayerLoadouts []RoundPlayerLoadoutData
	Compositions        []CompositionData // Compositions for CA denormalized stats
}

// RoundData holds round information.
type RoundData struct {
	ID           uuid.UUID
	RoundNumber  int16
	WinnerTeamID *uuid.UUID
	WinningTeam  *string
	WinMethod    *string
	SpikeEvent   *string
	PlantTimeMS  *int
}

// MatchPlayerData holds match player information with agent.
type MatchPlayerData struct {
	ID        uuid.UUID
	MatchID   uuid.UUID
	PlayerID  uuid.UUID
	TeamID    *uuid.UUID
	TeamTag   string
	AgentID   *uuid.UUID
	AgentName string
}

// PlayerData holds player identity information.
type PlayerData struct {
	ID    uuid.UUID
	PUUID string
	Name  string
}

// RoundEventData holds round event information (kills and damage).
type RoundEventData struct {
	ID             uuid.UUID
	RoundID        uuid.UUID
	MatchID        uuid.UUID
	TimestampMS    int
	EventType      string
	PlayerID       uuid.UUID
	VictimID       *uuid.UUID
	DamageGiven    *int
	Headshot       *int
	Bodyshot       *int
	Legshot        *int
	Weapon         *string     // "Spike" for bomb deaths, ability name, or nil for weapons
	WeaponID       *uuid.UUID  // Reference to asset_weapons table
	WeaponCategory *string     // Weapon category (Rifle, SMG, etc.) from asset_weapons
	Assistants     []uuid.UUID // Player UUIDs who assisted in this kill
}

// RoundPlayerStateData holds player state per round.
type RoundPlayerStateData struct {
	ID       uuid.UUID
	RoundID  uuid.UUID
	PlayerID uuid.UUID
	Score    *int
}

// RoundPlayerLoadoutData holds loadout and economy information.
type RoundPlayerLoadoutData struct {
	RoundPlayerID uuid.UUID
	LoadoutID     *uuid.UUID
	Value         *int
	Remaining     *int
	Spent         *int
}

// BuildAggregates computes all aggregate statistics for a match.
// This is the main orchestrator that coordinates all calculations in the correct order.
func BuildAggregates(data *MatchData) (*AggregateSet, error) {
	now := time.Now().UTC()

	// Build helper maps
	playerTeam := BuildPlayerTeamMap(data.MatchPlayers)
	teamPlayers := BuildTeamPlayersMap(data.MatchPlayers)
	teamTagMap := BuildTeamTagMap(data.MatchPlayers)
	teamIDs := GetTeamIDs(data.MatchPlayers)

	// Step 1: Compute trades (depends only on events)
	trades := ComputeTrades(data.RoundEvents, playerTeam)

	// Step 2: Compute entry kills (depends only on events)
	entries := ComputeEntries(data.Rounds, data.RoundEvents)

	// Step 3: Compute clutches (depends on events + rounds + players)
	clutchResults := ComputeClutches(data.Rounds, data.RoundEvents, playerTeam, teamPlayers, teamTagMap)

	// Step 4: Compute multi-kills (depends only on events + playerTeam)
	multiKills := ComputeMultiKills(data.RoundEvents, playerTeam)

	// Step 5: Compute duels (head-to-head stats between players)
	duels := ComputeDuels(data.RoundEvents, entries, playerTeam)

	// Step 6: Compute weapon stats (per-weapon stats for each player)
	weaponStats := ComputeWeaponStats(data.RoundEvents, entries, playerTeam)

	// Step 7: Build round player stats (uses trades, entries, clutches, playerTeam for suicide/teamkill)
	roundPlayerStats := BuildRoundPlayerStats(data, trades, entries, clutchResults, playerTeam, now)

	// Step 7.5: Build round team stats (aggregates round player stats per team for composition CAs)
	roundTeamStats := BuildRoundTeamStats(data, data.Rounds, roundPlayerStats, playerTeam, teamIDs, teamTagMap, now)

	// Step 8: Build match player stats (aggregates round stats + clutches + multi-kills + Phase 5 columns)
	matchPlayerStats := BuildMatchPlayerStats(data, roundPlayerStats, clutchResults, multiKills, entries, trades, playerTeam, now)

	// Step 9: Build team match stats (aggregates round stats by team)
	teamMatchStats := BuildTeamMatchStats(data, data.Rounds, roundPlayerStats, clutchResults, multiKills, playerTeam, teamIDs, teamTagMap, now)

	// Step 10: Build team side stats (filters by Attack/Defense)
	teamMatchSideStats := BuildTeamMatchSideStats(data, data.Rounds, roundPlayerStats, clutchResults, multiKills, playerTeam, teamIDs, teamTagMap, now)

	// Step 11: Build duels rows for database
	matchPlayerDuels := BuildMatchPlayerDuels(data.MatchID, duels, now)

	// Step 12: Build weapon stats rows for database
	matchPlayerWeaponStats := BuildMatchPlayerWeaponStats(data.MatchID, data.MatchDate, weaponStats, now)

	// Build clutch rows for database
	clutchRows := buildClutchRows(clutchResults, playerTeam, teamPlayers, now)

	// Update round player stats with clutch IDs
	roundPlayerStats = linkClutchesToRoundStats(roundPlayerStats, clutchRows)

	// Step 13: Build denormalized stats for CA support (replaces PostgreSQL MVs)
	playerClutchStats := BuildPlayerClutchStats(data.MatchID, data.MatchDate, matchPlayerStats, now)
	compositionWeaponStats := BuildCompositionWeaponStats(data.MatchID, data.MatchDate, data.RoundEvents, data.Compositions, playerTeam, now)
	compositionClutchStats := BuildCompositionClutchStats(data.MatchID, data.MatchDate, clutchResults, data.Compositions, playerTeam, now)

	return &AggregateSet{
		MatchID:                   data.MatchID,
		Clutches:                  clutchRows,
		RoundPlayerStats:          roundPlayerStats,
		RoundTeamStats:            roundTeamStats,
		MatchPlayerStats:          matchPlayerStats,
		TeamMatchStats:            teamMatchStats,
		TeamMatchSideStats:        teamMatchSideStats,
		MatchPlayerDuels:          matchPlayerDuels,
		MatchPlayerWeaponStats:    matchPlayerWeaponStats,
		PlayerClutchStats:         playerClutchStats,
		CompositionWeaponStats:    compositionWeaponStats,
		CompositionClutchStats:    compositionClutchStats,
	}, nil
}

// buildClutchRows converts ClutchResult to ClutchRow for database insertion.
// Creates rows for both the clutcher (is_clutcher=true) and opponents (is_clutcher=false).
func buildClutchRows(clutchResults []ClutchResult, playerTeam map[uuid.UUID]uuid.UUID, teamPlayers map[uuid.UUID][]uuid.UUID, now time.Time) []ClutchRow {
	var rows []ClutchRow

	for _, cr := range clutchResults {
		clutchType := cr.Type
		won := cr.Won
		side := cr.Side
		situation := cr.Situation
		clutchStartTimeMS := cr.ClutchStartTimeMS
		clutchEndTimeMS := cr.ClutchEndTimeMS

		// Create row for clutcher
		isClutcherTrue := true
		rows = append(rows, ClutchRow{
			ID:                uuid.New(),
			RoundID:           cr.RoundID,
			PlayerID:          cr.ClutcherID,
			Side:              &side,
			Won:               &won,
			IsClutcher:        &isClutcherTrue,
			Situation:         &situation,
			Type:              &clutchType,
			ClutchStartTimeMS: &clutchStartTimeMS,
			ClutchEndTimeMS:   &clutchEndTimeMS,
			CreatedAt:         now,
		})

		// Create rows for opponents (they were in the clutch situation against)
		opponentWon := !won
		isClutcherFalse := false
		opponentSide := getOppositeSide(side)

		for _, opponentID := range cr.OpponentIDs {
			rows = append(rows, ClutchRow{
				ID:                uuid.New(),
				RoundID:           cr.RoundID,
				PlayerID:          opponentID,
				Side:              &opponentSide,
				Won:               &opponentWon,
				IsClutcher:        &isClutcherFalse,
				Situation:         &situation,
				Type:              &clutchType,
				ClutchStartTimeMS: &clutchStartTimeMS,
				ClutchEndTimeMS:   &clutchEndTimeMS,
				CreatedAt:         now,
			})
		}
	}

	return rows
}

// getOppositeSide returns the opposite side.
func getOppositeSide(side string) string {
	if side == "Attack" {
		return "Defense"
	}
	return "Attack"
}

// linkClutchesToRoundStats links clutch IDs to round player stats.
func linkClutchesToRoundStats(stats []RoundPlayerStatsRow, clutches []ClutchRow) []RoundPlayerStatsRow {
	// Build lookup: roundID+playerID -> clutchID (for clutcher only)
	clutchLookup := make(map[string]uuid.UUID)
	for _, c := range clutches {
		if c.IsClutcher != nil && *c.IsClutcher {
			key := c.RoundID.String() + ":" + c.PlayerID.String()
			clutchLookup[key] = c.ID
		}
	}

	// Update stats
	for i := range stats {
		key := stats[i].RoundID.String() + ":" + stats[i].PlayerID.String()
		if clutchID, ok := clutchLookup[key]; ok {
			stats[i].ClutchID = &clutchID
		}
	}

	return stats
}
