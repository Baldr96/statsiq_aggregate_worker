package aggregate

import (
	"github.com/google/uuid"
)

// ComputeClutches detects clutch situations in all rounds.
// A clutch occurs when a player is the last survivor of their team facing 1-5 opponents.
// The clutch must be confirmed after 3 seconds of being alone.
func ComputeClutches(rounds []RoundData, events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID, teamPlayers map[uuid.UUID][]uuid.UUID, teamTagMap map[uuid.UUID]string) []ClutchResult {
	// Group events by round
	eventsByRound := make(map[uuid.UUID][]RoundEventData)
	for _, e := range events {
		eventsByRound[e.RoundID] = append(eventsByRound[e.RoundID], e)
	}

	var results []ClutchResult

	for _, round := range rounds {
		clutch := detectClutchInRound(round, eventsByRound[round.ID], playerTeam, teamPlayers, teamTagMap)
		if clutch != nil {
			results = append(results, *clutch)
		}
	}

	return results
}

// detectClutchInRound detects a clutch situation in a single round.
func detectClutchInRound(round RoundData, events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID, teamPlayers map[uuid.UUID][]uuid.UUID, teamTagMap map[uuid.UUID]string) *ClutchResult {
	// Initialize all players as alive
	alive := make(map[uuid.UUID]bool)
	for _, players := range teamPlayers {
		for _, playerID := range players {
			alive[playerID] = true
		}
	}

	// Track clutch states by team
	clutchStates := make(map[uuid.UUID]*ClutchState)
	var confirmedClutch *ClutchState
	var clutcherTeamID uuid.UUID

	// Filter kills only, sorted by timestamp
	var kills []RoundEventData
	for _, e := range events {
		if e.EventType == "kill" && e.VictimID != nil {
			kills = append(kills, e)
		}
	}

	lastTimestamp := 0
	for _, kill := range kills {
		victimID := *kill.VictimID
		victimTeam, hasTeam := playerTeam[victimID]
		if !hasTeam {
			continue
		}

		// Mark victim as dead
		alive[victimID] = false
		lastTimestamp = kill.TimestampMS

		// Count survivors on victim's team
		aliveCount := countAlive(teamPlayers[victimTeam], alive)

		if aliveCount == 1 {
			// Find the survivor
			survivor := findSurvivor(teamPlayers[victimTeam], alive)
			if survivor == uuid.Nil {
				continue
			}

			// Count opponents alive
			opponentTeam := GetOtherTeamID(victimTeam, teamPlayers)
			opponentsAlive := countAlive(teamPlayers[opponentTeam], alive)

			if opponentsAlive > 0 && opponentsAlive <= 5 {
				// Potential clutch situation
				side := DetermineSide(round.RoundNumber, victimTeam, teamTagMap)
				situation := determineSituation(round, kill.TimestampMS)

				// Get IDs of opponents who are alive at clutch start
				aliveOpponentIDs := getAlivePlayerIDs(teamPlayers[opponentTeam], alive)

				clutchStates[victimTeam] = &ClutchState{
					Candidate:        survivor,
					AloneSince:       kill.TimestampMS,
					OpponentsAtStart: opponentsAlive,
					AliveOpponentIDs: aliveOpponentIDs,
					Side:             side,
					Situation:        situation,
					Confirmed:        false,
				}
			}
		}

		// Check if any clutch should be confirmed (3s elapsed)
		for teamID, state := range clutchStates {
			if !state.Confirmed {
				elapsed := kill.TimestampMS - state.AloneSince
				if elapsed >= ClutchIdentificationDelayMS {
					state.Confirmed = true
					confirmedClutch = state
					clutcherTeamID = teamID
				}
			}
		}

		// Check if clutcher died before confirmation
		for teamID, state := range clutchStates {
			if !state.Confirmed && victimID == state.Candidate {
				// Clutcher died too fast, invalidate
				delete(clutchStates, teamID)
			}
		}
	}

	// If no clutch was confirmed during kills, check remaining states at round end
	if confirmedClutch == nil {
		for teamID, state := range clutchStates {
			if !state.Confirmed && alive[state.Candidate] {
				// Check if enough time has passed since becoming alone
				// Use a reasonable round end estimate
				roundEndMS := lastTimestamp + 1000
				elapsed := roundEndMS - state.AloneSince
				if elapsed >= ClutchIdentificationDelayMS {
					state.Confirmed = true
					confirmedClutch = state
					clutcherTeamID = teamID
				}
			}
		}
	}

	if confirmedClutch == nil {
		return nil
	}

	// Determine if clutch was won
	won := false
	if round.WinnerTeamID != nil && *round.WinnerTeamID == clutcherTeamID {
		won = true
	}

	// Use the alive opponent IDs from clutch state (captured at clutch start)
	return &ClutchResult{
		RoundID:           round.ID,
		ClutcherID:        confirmedClutch.Candidate,
		OpponentIDs:       confirmedClutch.AliveOpponentIDs,
		Type:              confirmedClutch.OpponentsAtStart,
		Won:               won,
		Side:              confirmedClutch.Side,
		Situation:         confirmedClutch.Situation,
		ClutchStartTimeMS: confirmedClutch.AloneSince,
		ClutchEndTimeMS:   lastTimestamp,
	}
}

// countAlive counts the number of alive players in a list.
func countAlive(playerIDs []uuid.UUID, alive map[uuid.UUID]bool) int {
	count := 0
	for _, id := range playerIDs {
		if alive[id] {
			count++
		}
	}
	return count
}

// findSurvivor finds the single surviving player in a list.
func findSurvivor(playerIDs []uuid.UUID, alive map[uuid.UUID]bool) uuid.UUID {
	for _, id := range playerIDs {
		if alive[id] {
			return id
		}
	}
	return uuid.Nil
}

// getAlivePlayerIDs returns the IDs of players who are alive.
func getAlivePlayerIDs(playerIDs []uuid.UUID, alive map[uuid.UUID]bool) []uuid.UUID {
	var result []uuid.UUID
	for _, id := range playerIDs {
		if alive[id] {
			result = append(result, id)
		}
	}
	return result
}

// determineSituation returns "post-plant" or "pre-plant" based on timestamp.
func determineSituation(round RoundData, currentTimeMS int) string {
	if round.PlantTimeMS != nil && currentTimeMS > *round.PlantTimeMS {
		return "post-plant"
	}
	return "pre-plant"
}
