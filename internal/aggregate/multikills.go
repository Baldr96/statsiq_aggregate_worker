package aggregate

import (
	"sort"

	"github.com/google/uuid"
)

// MultiKillWindowMS is the maximum time between consecutive kills to count as a multi-kill streak.
const MultiKillWindowMS = 5000 // 5 seconds

// MultiKillResult contains multi-kill statistics for a player in a round.
type MultiKillResult struct {
	PlayerID    uuid.UUID
	RoundID     uuid.UUID
	MultiKills  int // Total multi-kill streaks (2+)
	DoubleKills int // Streaks of exactly 2 kills
	TripleKills int // Streaks of exactly 3 kills
	QuadraKills int // Streaks of exactly 4 kills
	PentaKills  int // Streaks of 5+ kills
}

// ComputeMultiKills detects multi-kill streaks across all rounds.
// A multi-kill is a sequence of kills by the same player where each consecutive kill
// happens within 5 seconds of the previous one.
// Returns a map of roundID -> playerID -> MultiKillResult.
func ComputeMultiKills(events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID) map[uuid.UUID]map[uuid.UUID]*MultiKillResult {
	// Group events by round
	eventsByRound := make(map[uuid.UUID][]RoundEventData)
	for _, e := range events {
		eventsByRound[e.RoundID] = append(eventsByRound[e.RoundID], e)
	}

	// results[roundID][playerID] = MultiKillResult
	results := make(map[uuid.UUID]map[uuid.UUID]*MultiKillResult)

	for roundID, roundEvents := range eventsByRound {
		results[roundID] = computeRoundMultiKills(roundID, roundEvents, playerTeam)
	}

	return results
}

// computeRoundMultiKills computes multi-kills for a single round.
func computeRoundMultiKills(roundID uuid.UUID, events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID) map[uuid.UUID]*MultiKillResult {
	// Filter valid kills (exclude self-kills, teamkills, spike deaths)
	var kills []RoundEventData
	for _, e := range events {
		if e.EventType != "kill" || e.VictimID == nil {
			continue
		}

		killerID := e.PlayerID
		victimID := *e.VictimID

		// Skip self-kills (suicides and spike deaths)
		if killerID == victimID {
			continue
		}

		// Skip teamkills
		killerTeam, killerHasTeam := playerTeam[killerID]
		victimTeam, victimHasTeam := playerTeam[victimID]
		if killerHasTeam && victimHasTeam && killerTeam == victimTeam {
			continue
		}

		kills = append(kills, e)
	}

	// Group kills by player
	killsByPlayer := make(map[uuid.UUID][]RoundEventData)
	for _, k := range kills {
		killsByPlayer[k.PlayerID] = append(killsByPlayer[k.PlayerID], k)
	}

	// Compute multi-kills for each player
	results := make(map[uuid.UUID]*MultiKillResult)

	for playerID, playerKills := range killsByPlayer {
		result := &MultiKillResult{
			PlayerID: playerID,
			RoundID:  roundID,
		}

		// Sort kills by timestamp
		sort.Slice(playerKills, func(i, j int) bool {
			return playerKills[i].TimestampMS < playerKills[j].TimestampMS
		})

		// Detect multi-kill streaks
		if len(playerKills) >= 2 {
			streakStart := 0
			for i := 1; i <= len(playerKills); i++ {
				// Check if streak continues or ends
				continuesStreak := false
				if i < len(playerKills) {
					timeDiff := playerKills[i].TimestampMS - playerKills[i-1].TimestampMS
					continuesStreak = timeDiff <= MultiKillWindowMS
				}

				if !continuesStreak {
					// Streak ended at index i-1
					streakLength := i - streakStart
					if streakLength >= 2 {
						result.MultiKills++
						switch streakLength {
						case 2:
							result.DoubleKills++
						case 3:
							result.TripleKills++
						case 4:
							result.QuadraKills++
						default: // 5+
							result.PentaKills++
						}
					}
					streakStart = i
				}
			}
		}

		if result.MultiKills > 0 {
			results[playerID] = result
		}
	}

	return results
}

// AggregateMultiKills aggregates multi-kill results from round level to match level for a player.
func AggregateMultiKills(roundResults map[uuid.UUID]map[uuid.UUID]*MultiKillResult, playerID uuid.UUID) *MultiKillResult {
	result := &MultiKillResult{PlayerID: playerID}

	for _, roundResult := range roundResults {
		if pr, ok := roundResult[playerID]; ok {
			result.MultiKills += pr.MultiKills
			result.DoubleKills += pr.DoubleKills
			result.TripleKills += pr.TripleKills
			result.QuadraKills += pr.QuadraKills
			result.PentaKills += pr.PentaKills
		}
	}

	return result
}

// AggregateMultiKillsForRounds aggregates multi-kill results for a player across specific rounds only.
func AggregateMultiKillsForRounds(roundResults map[uuid.UUID]map[uuid.UUID]*MultiKillResult, playerID uuid.UUID, roundIDs []uuid.UUID) *MultiKillResult {
	result := &MultiKillResult{PlayerID: playerID}

	// Create a set of round IDs for fast lookup
	roundSet := make(map[uuid.UUID]bool)
	for _, id := range roundIDs {
		roundSet[id] = true
	}

	for roundID, roundResult := range roundResults {
		if !roundSet[roundID] {
			continue
		}
		if pr, ok := roundResult[playerID]; ok {
			result.MultiKills += pr.MultiKills
			result.DoubleKills += pr.DoubleKills
			result.TripleKills += pr.TripleKills
			result.QuadraKills += pr.QuadraKills
			result.PentaKills += pr.PentaKills
		}
	}

	return result
}
