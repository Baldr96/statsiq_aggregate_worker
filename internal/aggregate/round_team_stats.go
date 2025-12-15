package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildRoundTeamStats constructs team-level statistics for each round.
// Aggregates from round_player_stats per team per round for composition CA support.
// This eliminates the need for LATERAL joins in composition continuous aggregates.
func BuildRoundTeamStats(
	data *MatchData,
	rounds []RoundData,
	roundPlayerStats []RoundPlayerStatsRow,
	playerTeam map[uuid.UUID]uuid.UUID,
	teamIDs []uuid.UUID,
	teamTagMap map[uuid.UUID]string,
	now time.Time,
) []RoundTeamStatsRow {
	// Group round player stats by round and team
	statsByRoundTeam := make(map[uuid.UUID]map[uuid.UUID][]RoundPlayerStatsRow)
	for _, rps := range roundPlayerStats {
		teamID, ok := playerTeam[rps.PlayerID]
		if !ok {
			continue
		}
		if statsByRoundTeam[rps.RoundID] == nil {
			statsByRoundTeam[rps.RoundID] = make(map[uuid.UUID][]RoundPlayerStatsRow)
		}
		statsByRoundTeam[rps.RoundID][teamID] = append(statsByRoundTeam[rps.RoundID][teamID], rps)
	}

	var rows []RoundTeamStatsRow

	for _, round := range rounds {
		for _, teamID := range teamIDs {
			teamStats := statsByRoundTeam[round.ID][teamID]
			if len(teamStats) == 0 {
				continue
			}

			// Aggregate all player stats for this team in this round
			var kills, deaths, assists int
			var damageGiven, damageTaken int
			var creditsSpent, creditsRemaining int
			var firstKills, firstDeaths int
			var tradeKills, tradedDeaths int

			for _, rs := range teamStats {
				kills += int(rs.Kills)
				deaths += int(rs.Deaths)
				assists += int(rs.Assists)
				damageGiven += rs.DamageGiven
				damageTaken += rs.DamageTaken
				creditsSpent += rs.CreditsSpent
				creditsRemaining += rs.CreditsRemaining
				tradeKills += rs.TradeKill
				tradedDeaths += rs.TradedDeath

				if rs.FirstKill {
					firstKills++
				}
				if rs.FirstDeath {
					firstDeaths++
				}
			}

			// Determine team tag from the teamTagMap (uppercase if needed)
			teamTag := teamTagMap[teamID]
			if teamTag == "Red" {
				teamTag = "RED"
			} else if teamTag == "Blue" {
				teamTag = "BLUE"
			}

			// Determine side (Attack/Defense)
			side := DetermineSide(round.RoundNumber, teamID, teamTagMap)

			// Determine buy type based on total team spend
			buyType := classifyBuyType(round.RoundNumber, creditsSpent)

			// Determine situation based on side and plant status
			spikeWasPlanted := round.PlantTimeMS != nil
			situation := classifySituation(side, spikeWasPlanted)

			// Determine if round was won
			roundWon := round.WinnerTeamID != nil && *round.WinnerTeamID == teamID

			rows = append(rows, RoundTeamStatsRow{
				ID:               uuid.New(),
				RoundID:          round.ID,
				MatchDate:        data.MatchDate,
				TeamID:           teamID,
				TeamTag:          teamTag,
				CreditsSpent:     creditsSpent,
				CreditsRemaining: creditsRemaining,
				BuyType:          buyType,
				Kills:            kills,
				Deaths:           deaths,
				Assists:          assists,
				DamageGiven:      damageGiven,
				DamageTaken:      damageTaken,
				FirstKills:       firstKills,
				FirstDeaths:      firstDeaths,
				TradeKills:       tradeKills,
				TradedDeaths:     tradedDeaths,
				Side:             side,
				Situation:        situation,
				RoundWon:         roundWon,
				IsOvertime:       IsOvertimeRound(round.RoundNumber),
				CreatedAt:        now,
			})
		}
	}

	return rows
}

// classifyBuyType determines economy classification based on round and team spend.
// Pistol rounds (0, 12) are always classified as "Pistol".
// Otherwise classification is based on total team credits spent.
func classifyBuyType(roundNumber int16, creditsSpent int) string {
	// Pistol rounds (first round of each half)
	if roundNumber == 0 || roundNumber == 12 {
		return "Pistol"
	}

	if creditsSpent < 1000 {
		return "DRY"
	}
	if creditsSpent < 3000 {
		return "ECO"
	}
	if creditsSpent < 15000 {
		return "SEMI"
	}
	return "FULL"
}

// classifySituation determines tactical situation based on side and plant status.
// Attack side: Pre-Plant (no plant) or Post-Plant (spike planted)
// Defense side: Def Holds (no plant) or Def Retakes (spike planted)
func classifySituation(side string, spikeWasPlanted bool) string {
	if side == "Attack" {
		if spikeWasPlanted {
			return "Post-Plant"
		}
		return "Pre-Plant"
	}
	// Defense
	if spikeWasPlanted {
		return "Def Retakes"
	}
	return "Def Holds"
}
