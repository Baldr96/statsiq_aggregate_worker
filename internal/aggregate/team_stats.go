package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildTeamMatchStats constructs team-level statistics for the match.
func BuildTeamMatchStats(
	data *MatchData,
	rounds []RoundData,
	roundPlayerStats []RoundPlayerStatsRow,
	clutches []ClutchResult,
	multiKillResults map[uuid.UUID]map[uuid.UUID]*MultiKillResult,
	playerTeam map[uuid.UUID]uuid.UUID,
	teamIDs []uuid.UUID,
	teamTagMap map[uuid.UUID]string,
	now time.Time,
) []TeamMatchStatsRow {
	// Calculate match-level flags
	isOvertime := IsMatchOvertime(data.TeamRedScore, data.TeamBlueScore)

	// Group round stats by team AND by player for averaging
	statsByTeamPlayer := make(map[uuid.UUID]map[uuid.UUID][]RoundPlayerStatsRow)
	for _, teamID := range teamIDs {
		statsByTeamPlayer[teamID] = make(map[uuid.UUID][]RoundPlayerStatsRow)
	}
	for _, rps := range roundPlayerStats {
		teamID, ok := playerTeam[rps.PlayerID]
		if !ok {
			continue
		}
		statsByTeamPlayer[teamID][rps.PlayerID] = append(statsByTeamPlayer[teamID][rps.PlayerID], rps)
	}

	// Analyze rounds for each team
	roundInfoByTeam := analyzeRoundsForTeams(rounds, teamIDs, teamTagMap)

	// Group clutches by team (clutcher only)
	clutchesByTeam := make(map[uuid.UUID][]ClutchResult)
	for _, c := range clutches {
		teamID, ok := playerTeam[c.ClutcherID]
		if ok {
			clutchesByTeam[teamID] = append(clutchesByTeam[teamID], c)
		}
	}

	var rows []TeamMatchStatsRow

	for _, teamID := range teamIDs {
		teamPlayerStats := statsByTeamPlayer[teamID]
		teamRoundInfo := roundInfoByTeam[teamID]
		teamClutches := clutchesByTeam[teamID]

		// Aggregate stats from all players on the team
		var totalKills, totalDeaths, totalAssists int
		var totalDamageGiven, totalDamageTaken int
		var totalFirstKills, totalFirstDeaths int
		var totalTradeKills, totalTradedDeaths int
		var totalSuicides, totalTeamKills, totalDeathsBySpike int
		totalChainKills := 0
		totalDoubleKills := 0
		totalTripleKills := 0
		totalQuadraKills := 0
		totalPentaKills := 0

		// Calculate per-player ratios for averaging
		type PlayerRatios struct {
			KPR, DPR, APR, ADR, ACS float64
		}
		var playerRatios []PlayerRatios

		for playerID, playerRoundStats := range teamPlayerStats {
			var pKills, pDeaths, pAssists, pDamage int
			var pCS float64 // Total Combat Score for this player
			pRounds := len(playerRoundStats)

			for _, rs := range playerRoundStats {
				pKills += int(rs.Kills)
				pDeaths += int(rs.Deaths)
				pAssists += int(rs.Assists)
				pDamage += rs.DamageGiven
				pCS += rs.CS

				totalKills += int(rs.Kills)
				totalDeaths += int(rs.Deaths)
				totalAssists += int(rs.Assists)
				totalDamageGiven += rs.DamageGiven
				totalDamageTaken += rs.DamageTaken
				totalTradeKills += rs.TradeKill
				totalTradedDeaths += rs.TradedDeath

				if rs.FirstKill {
					totalFirstKills++
				}
				if rs.FirstDeath {
					totalFirstDeaths++
				}
				totalSuicides += rs.Suicides
				if rs.KilledBySpike {
					totalDeathsBySpike++
				}
				totalTeamKills += rs.TeammatesKilled
			}

			// Aggregate multi-kills for this player using time-based detection
			playerMultiKills := AggregateMultiKills(multiKillResults, playerID)
			totalChainKills += playerMultiKills.MultiKills
			totalDoubleKills += playerMultiKills.DoubleKills
			totalTripleKills += playerMultiKills.TripleKills
			totalQuadraKills += playerMultiKills.QuadraKills
			totalPentaKills += playerMultiKills.PentaKills

			// Calculate individual player ratios
			if pRounds > 0 {
				playerRatios = append(playerRatios, PlayerRatios{
					KPR: float64(pKills) / float64(pRounds),
					DPR: float64(pDeaths) / float64(pRounds),
					APR: float64(pAssists) / float64(pRounds),
					ADR: float64(pDamage) / float64(pRounds),
					ACS: pCS / float64(pRounds), // Average Combat Score per round
				})
			}
		}

		roundsPlayed := teamRoundInfo.Total
		roundsWon := teamRoundInfo.Won
		roundsLost := roundsPlayed - roundsWon

		// Calculate averages as mean of player ratios
		var avgKPR, avgDPR, avgAPR, avgADR, avgACS float64
		if len(playerRatios) > 0 {
			for _, pr := range playerRatios {
				avgKPR += pr.KPR
				avgDPR += pr.DPR
				avgAPR += pr.APR
				avgADR += pr.ADR
				avgACS += pr.ACS
			}
			n := float64(len(playerRatios))
			avgKPR /= n
			avgDPR /= n
			avgAPR /= n
			avgADR /= n
			avgACS /= n
		}

		var kd, roundWinRate, damageDelta float64

		if roundsPlayed > 0 {
			roundWinRate = float64(roundsWon) / float64(roundsPlayed) * 100
		}

		if totalDeaths > 0 {
			kd = float64(totalKills) / float64(totalDeaths)
		}

		damageDelta = float64(totalDamageGiven - totalDamageTaken)

		// Clutch stats
		clutchesPlayed := len(teamClutches)
		clutchesWon := 0
		for _, c := range teamClutches {
			if c.Won {
				clutchesWon++
			}
		}
		clutchesLoss := clutchesPlayed - clutchesWon

		var clutchesWR float64
		if clutchesPlayed > 0 {
			clutchesWR = float64(clutchesWon) / float64(clutchesPlayed) * 100
		}

		// Calculate match_won based on team tag
		matchWon := false
		teamTag := teamTagMap[teamID]
		if (teamTag == "Red" || teamTag == "RED") && data.TeamRedScore > data.TeamBlueScore {
			matchWon = true
		} else if (teamTag == "Blue" || teamTag == "BLUE") && data.TeamBlueScore > data.TeamRedScore {
			matchWon = true
		}

		// Count overtime rounds won/lost
		var roundsOvertimeWon, roundsOvertimeLost int
		for _, round := range rounds {
			if !IsOvertimeRound(round.RoundNumber) {
				continue
			}
			if round.WinnerTeamID != nil && *round.WinnerTeamID == teamID {
				roundsOvertimeWon++
			} else {
				roundsOvertimeLost++
			}
		}

		rows = append(rows, TeamMatchStatsRow{
			ID:                  uuid.New(),
			TeamID:              teamID,
			MatchID:             data.MatchID,
			MatchDate:           data.MatchDate,
			MatchType:           data.MatchType,
			RoundsPlayed:        roundsPlayed,
			RoundsWon:           roundsWon,
			RoundsLost:          roundsLost,
			RoundWinRate:        roundWinRate,
			KD:                  kd,
			AvgKPR:              avgKPR,
			AvgDPR:              avgDPR,
			AvgAPR:              avgAPR,
			AvgADR:              avgADR,
			AvgACS:              avgACS,
			DamageDelta:         damageDelta,
			Kills:               totalKills,
			Deaths:              totalDeaths,
			FirstKills:          totalFirstKills,
			FirstDeaths:         totalFirstDeaths,
			TradeKills:          totalTradeKills,
			TradedDeaths:        totalTradedDeaths,
			Suicides:            totalSuicides,
			TeammatesKilled:     totalTeamKills,
			DeathsBySpike:       totalDeathsBySpike,
			ChainKills:          totalChainKills,
			DoubleKills:         totalDoubleKills,
			TripleKills:         totalTripleKills,
			QuadraKills:         totalQuadraKills,
			PentaKills:          totalPentaKills,
			MultiKills:          totalDoubleKills + totalTripleKills + totalQuadraKills + totalPentaKills,
			ClutchesPlayed:      clutchesPlayed,
			ClutchesWon:         clutchesWon,
			ClutchesLoss:        clutchesLoss,
			ClutchesWR:          clutchesWR,
			AttackRoundsWin:     teamRoundInfo.AttackWon,
			AttackRoundsPlayed:  teamRoundInfo.AttackPlayed,
			DefenseRoundsWins:   teamRoundInfo.DefenseWon,
			DefenseRoundsPlayed: teamRoundInfo.DefensePlayed,
			MatchWon:            matchWon,
			IsOvertime:          isOvertime,
			RoundsOvertimeWon:   roundsOvertimeWon,
			RoundsOvertimeLost:  roundsOvertimeLost,
			CreatedAt:           now,
		})
	}

	return rows
}

// analyzeRoundsForTeams analyzes round wins/losses by team and side.
func analyzeRoundsForTeams(rounds []RoundData, teamIDs []uuid.UUID, teamTagMap map[uuid.UUID]string) map[uuid.UUID]*TeamRoundInfo {
	info := make(map[uuid.UUID]*TeamRoundInfo)
	for _, teamID := range teamIDs {
		info[teamID] = &TeamRoundInfo{}
	}

	for _, round := range rounds {
		for _, teamID := range teamIDs {
			teamInfo := info[teamID]
			teamInfo.Total++

			side := DetermineSide(round.RoundNumber, teamID, teamTagMap)
			if side == "Attack" {
				teamInfo.AttackPlayed++
			} else {
				teamInfo.DefensePlayed++
			}

			// Check if team won
			if round.WinnerTeamID != nil && *round.WinnerTeamID == teamID {
				teamInfo.Won++
				if side == "Attack" {
					teamInfo.AttackWon++
				} else {
					teamInfo.DefenseWon++
				}
			}
		}
	}

	return info
}


