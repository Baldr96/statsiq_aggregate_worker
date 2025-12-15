package aggregate

import (
	"github.com/google/uuid"
)

// ComputeTrades calculates trade kills and traded deaths for all players across all rounds.
// A trade kill occurs when a player kills an opponent who recently killed a teammate (within 3s).
// A traded death occurs when a player dies but a teammate kills the killer within 3s.
func ComputeTrades(events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID) map[uuid.UUID]map[uuid.UUID]*TradeResult {
	// Group events by round
	eventsByRound := make(map[uuid.UUID][]RoundEventData)
	for _, e := range events {
		eventsByRound[e.RoundID] = append(eventsByRound[e.RoundID], e)
	}

	// results[roundID][playerID] = TradeResult
	results := make(map[uuid.UUID]map[uuid.UUID]*TradeResult)

	for roundID, roundEvents := range eventsByRound {
		// Filter kills only
		var kills []RoundEventData
		for _, e := range roundEvents {
			if e.EventType == "kill" && e.VictimID != nil {
				kills = append(kills, e)
			}
		}

		results[roundID] = make(map[uuid.UUID]*TradeResult)

		for i, kill := range kills {
			killerID := kill.PlayerID
			victimID := *kill.VictimID
			killTime := kill.TimestampMS

			// Ensure player entries exist
			if results[roundID][killerID] == nil {
				results[roundID][killerID] = &TradeResult{PlayerID: killerID, RoundID: roundID}
			}
			if results[roundID][victimID] == nil {
				results[roundID][victimID] = &TradeResult{PlayerID: victimID, RoundID: roundID}
			}

			// ========================================
			// TRADED DEATH (Forward Looking)
			// Check if victim's death will be traded
			// ========================================
			for j := i + 1; j < len(kills); j++ {
				futureKill := kills[j]

				// Check if outside trade window
				if futureKill.TimestampMS > killTime+TradeWindowMS {
					break
				}

				// Check if killer gets killed
				if futureKill.VictimID != nil && *futureKill.VictimID == killerID {
					// Check if killer was killed by victim's teammate
					victimTeam, victimHasTeam := playerTeam[victimID]
					futureKillerTeam, futureKillerHasTeam := playerTeam[futureKill.PlayerID]

					if victimHasTeam && futureKillerHasTeam && futureKillerTeam == victimTeam {
						// Victim's death was traded
						results[roundID][victimID].TradedDeaths++
						break // A death can only be traded once
					}
				}
			}

			// ========================================
			// TRADE KILL (Backward Looking)
			// Check if this kill is a trade kill
			// ========================================
			for j := i - 1; j >= 0; j-- {
				pastKill := kills[j]

				// Check if outside trade window
				if pastKill.TimestampMS < killTime-TradeWindowMS {
					break
				}

				// Check if victim had killed someone
				if pastKill.PlayerID == victimID && pastKill.VictimID != nil {
					// Check if past victim was killer's teammate
					killerTeam, killerHasTeam := playerTeam[killerID]
					pastVictimTeam, pastVictimHasTeam := playerTeam[*pastKill.VictimID]

					if killerHasTeam && pastVictimHasTeam && pastVictimTeam == killerTeam {
						// This is a trade kill
						results[roundID][killerID].TradeKills++
						break // A kill counts as trade only once
					}
				}
			}
		}
	}

	return results
}

// BuildPlayerTeamMap creates a mapping from player ID to team ID.
func BuildPlayerTeamMap(matchPlayers []MatchPlayerData) map[uuid.UUID]uuid.UUID {
	playerTeam := make(map[uuid.UUID]uuid.UUID)
	for _, mp := range matchPlayers {
		if mp.TeamID != nil {
			playerTeam[mp.PlayerID] = *mp.TeamID
		}
	}
	return playerTeam
}

// BuildTeamPlayersMap creates a mapping from team ID to list of player IDs.
func BuildTeamPlayersMap(matchPlayers []MatchPlayerData) map[uuid.UUID][]uuid.UUID {
	teamPlayers := make(map[uuid.UUID][]uuid.UUID)
	for _, mp := range matchPlayers {
		if mp.TeamID != nil {
			teamPlayers[*mp.TeamID] = append(teamPlayers[*mp.TeamID], mp.PlayerID)
		}
	}
	return teamPlayers
}

// BuildTeamTagMap creates a mapping from team ID to team tag ("Red" or "Blue").
// This is used to determine the side (Attack/Defense) for a team.
func BuildTeamTagMap(matchPlayers []MatchPlayerData) map[uuid.UUID]string {
	teamTag := make(map[uuid.UUID]string)
	for _, mp := range matchPlayers {
		if mp.TeamID != nil && mp.TeamTag != "" {
			teamTag[*mp.TeamID] = mp.TeamTag
		}
	}
	return teamTag
}

// GetTeamIDs extracts the unique team IDs from the match players (typically 2 teams).
func GetTeamIDs(matchPlayers []MatchPlayerData) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var teamIDs []uuid.UUID
	for _, mp := range matchPlayers {
		if mp.TeamID != nil && !seen[*mp.TeamID] {
			seen[*mp.TeamID] = true
			teamIDs = append(teamIDs, *mp.TeamID)
		}
	}
	return teamIDs
}

// GetOtherTeamID returns the opposing team ID from the teamPlayers map.
// For a standard match with 2 teams, returns the team that is not the given one.
func GetOtherTeamID(teamID uuid.UUID, teamPlayers map[uuid.UUID][]uuid.UUID) uuid.UUID {
	for otherTeamID := range teamPlayers {
		if otherTeamID != teamID {
			return otherTeamID
		}
	}
	return uuid.Nil
}
