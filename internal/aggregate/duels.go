package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// DuelKey is a composite key for tracking duels between two players.
type DuelKey struct {
	PlayerID   uuid.UUID
	OpponentID uuid.UUID
}

// DuelStats tracks statistics between two players.
type DuelStats struct {
	Kills         int
	Deaths        int
	FirstKills    int
	FirstDeaths   int
	DamageGiven   int
	DamageTaken   int
	HeadshotKills int
}

// ComputeDuels calculates head-to-head statistics between all pairs of players.
// Returns a map from DuelKey to DuelStats for aggregation into match_player_duels_agregate.
func ComputeDuels(
	events []RoundEventData,
	entries map[uuid.UUID]*EntryResult,
	playerTeam map[uuid.UUID]uuid.UUID,
) map[DuelKey]*DuelStats {
	duels := make(map[DuelKey]*DuelStats)

	getOrCreate := func(playerID, opponentID uuid.UUID) *DuelStats {
		key := DuelKey{PlayerID: playerID, OpponentID: opponentID}
		if duels[key] == nil {
			duels[key] = &DuelStats{}
		}
		return duels[key]
	}

	for _, e := range events {
		if e.VictimID == nil {
			continue
		}

		victimID := *e.VictimID

		// Skip self-kills
		if e.PlayerID == victimID {
			continue
		}

		// Skip teamkills (same team)
		killerTeam, killerHasTeam := playerTeam[e.PlayerID]
		victimTeam, victimHasTeam := playerTeam[victimID]
		if killerHasTeam && victimHasTeam && killerTeam == victimTeam {
			continue
		}

		switch e.EventType {
		case "kill":
			// Player killed opponent
			killerStats := getOrCreate(e.PlayerID, victimID)
			killerStats.Kills++

			// Check if headshot kill
			if e.Headshot != nil && *e.Headshot > 0 {
				killerStats.HeadshotKills++
			}

			// Opponent was killed by player (symmetric tracking)
			victimStats := getOrCreate(victimID, e.PlayerID)
			victimStats.Deaths++

		case "damage":
			if e.DamageGiven == nil {
				continue
			}

			// Player dealt damage to opponent
			attackerStats := getOrCreate(e.PlayerID, victimID)
			attackerStats.DamageGiven += *e.DamageGiven

			// Opponent took damage from player
			victimStats := getOrCreate(victimID, e.PlayerID)
			victimStats.DamageTaken += *e.DamageGiven
		}
	}

	// Process entry kills to track first kills/deaths in duels
	for _, entry := range entries {
		// Entry killer got a first kill against the victim
		killerStats := getOrCreate(entry.EntryKillerID, entry.EntryVictimID)
		killerStats.FirstKills++

		// Entry victim got a first death against the killer
		victimStats := getOrCreate(entry.EntryVictimID, entry.EntryKillerID)
		victimStats.FirstDeaths++
	}

	return duels
}

// BuildMatchPlayerDuels converts computed duel statistics into database rows.
func BuildMatchPlayerDuels(
	matchID uuid.UUID,
	duels map[DuelKey]*DuelStats,
	now time.Time,
) []MatchPlayerDuelsRow {
	var rows []MatchPlayerDuelsRow

	for key, stats := range duels {
		// Only include duels with actual interactions
		if stats.Kills == 0 && stats.Deaths == 0 && stats.DamageGiven == 0 && stats.DamageTaken == 0 {
			continue
		}

		rows = append(rows, MatchPlayerDuelsRow{
			ID:            uuid.New(),
			MatchID:       matchID,
			PlayerID:      key.PlayerID,
			OpponentID:    key.OpponentID,
			Kills:         stats.Kills,
			Deaths:        stats.Deaths,
			FirstKills:    stats.FirstKills,
			FirstDeaths:   stats.FirstDeaths,
			DamageGiven:   stats.DamageGiven,
			DamageTaken:   stats.DamageTaken,
			HeadshotKills: stats.HeadshotKills,
			CreatedAt:     now,
		})
	}

	return rows
}
