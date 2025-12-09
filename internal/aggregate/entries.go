package aggregate

import (
	"github.com/google/uuid"
)

// ComputeEntries detects the first kill (entry kill) of each round.
// Returns a map from round ID to EntryResult.
func ComputeEntries(rounds []RoundData, events []RoundEventData) map[uuid.UUID]*EntryResult {
	// Group kills by round
	killsByRound := make(map[uuid.UUID][]RoundEventData)
	for _, e := range events {
		if e.EventType == "kill" && e.VictimID != nil {
			killsByRound[e.RoundID] = append(killsByRound[e.RoundID], e)
		}
	}

	results := make(map[uuid.UUID]*EntryResult)

	for _, round := range rounds {
		kills := killsByRound[round.ID]

		if len(kills) == 0 {
			continue
		}

		// Find the kill with minimum timestamp
		firstKill := kills[0]
		for _, kill := range kills[1:] {
			if kill.TimestampMS < firstKill.TimestampMS {
				firstKill = kill
			}
		}

		results[round.ID] = &EntryResult{
			RoundID:       round.ID,
			EntryKillerID: firstKill.PlayerID,
			EntryVictimID: *firstKill.VictimID,
			TimestampMS:   firstKill.TimestampMS,
		}
	}

	return results
}
