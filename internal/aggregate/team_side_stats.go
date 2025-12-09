package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildTeamMatchSideStats constructs team statistics broken down by side (Attack/Defense).
func BuildTeamMatchSideStats(
	data *MatchData,
	rounds []RoundData,
	roundPlayerStats []RoundPlayerStatsRow,
	clutches []ClutchResult,
	playerTeam map[uuid.UUID]uuid.UUID,
	now time.Time,
) []TeamMatchSideStatsRow {
	// Calculate match-level overtime flag
	isMatchOvertime := IsMatchOvertime(data.TeamRedScore, data.TeamBlueScore)

	// Build round ID to round data map
	roundByID := make(map[uuid.UUID]RoundData)
	for _, r := range rounds {
		roundByID[r.ID] = r
	}

	var rows []TeamMatchSideStatsRow

	for _, teamID := range []uuid.UUID{RedTeamID, BlueTeamID} {
		for _, side := range []string{"Attack", "Defense"} {
			// Get round IDs for this team and side
			roundIDs := getRoundIDsForTeamSide(rounds, teamID, side)

			if len(roundIDs) == 0 {
				continue
			}

			// Group round player stats by player for this team/side
			playerStatsMap := make(map[uuid.UUID][]RoundPlayerStatsRow)
			for _, rps := range roundPlayerStats {
				pTeamID, ok := playerTeam[rps.PlayerID]
				if !ok || pTeamID != teamID {
					continue
				}
				for _, roundID := range roundIDs {
					if rps.RoundID == roundID {
						playerStatsMap[rps.PlayerID] = append(playerStatsMap[rps.PlayerID], rps)
						break
					}
				}
			}

			// Filter clutches for this side and team
			var sideClutches []ClutchResult
			for _, c := range clutches {
				if c.Side != side {
					continue
				}
				cTeamID, ok := playerTeam[c.ClutcherID]
				if ok && cTeamID == teamID {
					sideClutches = append(sideClutches, c)
				}
			}

			// Aggregate stats and calculate per-player ratios
			var totalKills, totalDeaths, totalAssists int
			var totalDamageGiven, totalDamageTaken int
			var totalFirstKills, totalFirstDeaths int
			var totalTradeKills, totalTradedDeaths int
			var totalSuicides, totalTeamKills, totalDeathsBySpike int
			multiKills := 0

			type PlayerRatios struct {
				KPR, DPR, APR, ADR, ACS float64
			}
			var playerRatios []PlayerRatios

			for _, playerRoundStats := range playerStatsMap {
				var pKills, pDeaths, pAssists, pDamage int
				pRounds := len(playerRoundStats)

				for _, rs := range playerRoundStats {
					pKills += int(rs.Kills)
					pDeaths += int(rs.Deaths)
					pAssists += int(rs.Assists)
					pDamage += rs.DamageGiven

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
					if rs.Suicide {
						totalSuicides++
					}
					if rs.KilledBySpike {
						totalDeathsBySpike++
					}
					totalTeamKills += rs.KilledTeammate
					if rs.Kills >= 2 {
						multiKills++
					}
				}

				// Calculate individual player ratios
				if pRounds > 0 {
					playerRatios = append(playerRatios, PlayerRatios{
						KPR: float64(pKills) / float64(pRounds),
						DPR: float64(pDeaths) / float64(pRounds),
						APR: float64(pAssists) / float64(pRounds),
						ADR: float64(pDamage) / float64(pRounds),
					})
				}
			}

			// Count rounds won/lost on this side
			roundsPlayed := len(roundIDs)
			roundsWon := 0
			var roundsOvertimeWon, roundsOvertimeLost int

			for _, roundID := range roundIDs {
				r := roundByID[roundID]
				if r.WinnerTeamID != nil && *r.WinnerTeamID == teamID {
					roundsWon++
					if IsOvertimeRound(r.RoundNumber) {
						roundsOvertimeWon++
					}
				} else {
					if IsOvertimeRound(r.RoundNumber) {
						roundsOvertimeLost++
					}
				}
			}
			roundsLost := roundsPlayed - roundsWon

			// Determine side outcome
			sideOutcome := "Tie"
			if roundsWon > roundsLost {
				sideOutcome = "Win"
			} else if roundsLost > roundsWon {
				sideOutcome = "Lose"
			}

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

			// Clutch stats for this side
			clutchesPlayed := len(sideClutches)
			clutchesWon := 0
			for _, c := range sideClutches {
				if c.Won {
					clutchesWon++
				}
			}
			clutchesLoss := clutchesPlayed - clutchesWon

			var clutchesWR float64
			if clutchesPlayed > 0 {
				clutchesWR = float64(clutchesWon) / float64(clutchesPlayed) * 100
			}

			rows = append(rows, TeamMatchSideStatsRow{
				ID:                 uuid.New(),
				TeamID:             teamID,
				MatchID:            data.MatchID,
				MatchType:          data.MatchType,
				TeamSide:           side,
				SideOutcome:        sideOutcome,
				RoundsPlayed:       roundsPlayed,
				RoundsWon:          roundsWon,
				RoundsLost:         roundsLost,
				RoundWinRate:       roundWinRate,
				KD:                 kd,
				AvgKPR:             avgKPR,
				AvgDPR:             avgDPR,
				AvgAPR:             avgAPR,
				AvgADR:             avgADR,
				AvgACS:             avgACS,
				DamageDelta:        damageDelta,
				TotalKills:         totalKills,
				TotalDeaths:        totalDeaths,
				FirstKills:         totalFirstKills,
				FirstDeaths:        totalFirstDeaths,
				TradeKills:         totalTradeKills,
				TradedDeaths:       totalTradedDeaths,
				Suicides:           totalSuicides,
				TeamKills:          totalTeamKills,
				DeathsBySpike:      totalDeathsBySpike,
				Multikill:          multiKills,
				ClutchesPlayed:     clutchesPlayed,
				ClutchesWon:        clutchesWon,
				ClutchesLoss:       clutchesLoss,
				ClutchesWR:         clutchesWR,
				IsMatchOvertime:    isMatchOvertime,
				RoundsOvertimeWon:  roundsOvertimeWon,
				RoundsOvertimeLost: roundsOvertimeLost,
				CreatedAt:          now,
			})
		}
	}

	return rows
}

// getRoundIDsForTeamSide returns round IDs where the team played the specified side.
func getRoundIDsForTeamSide(rounds []RoundData, teamID uuid.UUID, side string) []uuid.UUID {
	var roundIDs []uuid.UUID

	for _, round := range rounds {
		teamSide := DetermineSide(round.RoundNumber, teamID)
		if teamSide == side {
			roundIDs = append(roundIDs, round.ID)
		}
	}

	return roundIDs
}
