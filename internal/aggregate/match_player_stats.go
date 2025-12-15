package aggregate

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// BuildMatchPlayerStats aggregates round stats into match-level player statistics.
// Includes Phase 5 columns: rounds_won, first_kills_traded, first_deaths_traded, mvp, flawless_rounds.
func BuildMatchPlayerStats(
	data *MatchData,
	roundPlayerStats []RoundPlayerStatsRow,
	clutches []ClutchResult,
	multiKillResults map[uuid.UUID]map[uuid.UUID]*MultiKillResult,
	entries map[uuid.UUID]*EntryResult,
	trades map[uuid.UUID]map[uuid.UUID]*TradeResult,
	playerTeam map[uuid.UUID]uuid.UUID,
	now time.Time,
) []MatchPlayerStatsRow {
	// Calculate is_overtime at match level
	isOvertime := IsMatchOvertime(data.TeamRedScore, data.TeamBlueScore)

	// Group round stats by player
	statsByPlayer := make(map[uuid.UUID][]RoundPlayerStatsRow)
	for _, rps := range roundPlayerStats {
		statsByPlayer[rps.PlayerID] = append(statsByPlayer[rps.PlayerID], rps)
	}

	// Group round stats by round for flawless calculation
	statsByRound := make(map[uuid.UUID][]RoundPlayerStatsRow)
	for _, rps := range roundPlayerStats {
		statsByRound[rps.RoundID] = append(statsByRound[rps.RoundID], rps)
	}

	// Group clutches by player (clutcher only)
	clutchesByPlayer := make(map[uuid.UUID][]ClutchResult)
	for _, c := range clutches {
		clutchesByPlayer[c.ClutcherID] = append(clutchesByPlayer[c.ClutcherID], c)
	}

	// Pre-compute flawless rounds per team (rounds where team had 0 deaths)
	flawlessRoundsByTeam := computeFlawlessRounds(data.Rounds, statsByRound, playerTeam)

	// Build teamTagMap for this match
	teamTagMap := BuildTeamTagMap(data.MatchPlayers)

	// Determine winning team tag based on scores
	var winningTeamTag string
	if data.TeamRedScore > data.TeamBlueScore {
		winningTeamTag = "Red"
	} else if data.TeamBlueScore > data.TeamRedScore {
		winningTeamTag = "Blue"
	}
	// If tied, winningTeamTag is ""

	var rows []MatchPlayerStatsRow

	for playerID, roundStats := range statsByPlayer {
		// Aggregate stats from all rounds
		var totalKills, totalDeaths, totalAssists int
		var totalDamageGiven, totalDamageTaken int
		var totalFirstKills, totalFirstDeaths int
		var totalTradeKills, totalTradedDeaths int
		var totalHeadshotKills, totalBodyshotKills, totalLegshotKills int
		var totalHeadshotHit, totalBodyshotHit, totalLegshotHit int
		var totalSuicides, totalTeamKills, totalDeathsBySpike int
		var totalCS float64 // Sum of Combat Scores for ACS calculation
		kastRounds := 0

		for _, rs := range roundStats {
			totalCS += rs.CS
			totalKills += int(rs.Kills)
			totalDeaths += int(rs.Deaths)
			totalAssists += int(rs.Assists)
			totalDamageGiven += rs.DamageGiven
			totalDamageTaken += rs.DamageTaken
			totalTradeKills += rs.TradeKill
			totalTradedDeaths += rs.TradedDeath
			totalHeadshotKills += rs.HeadshotKills
			totalBodyshotKills += rs.BodyshotKills
			totalLegshotKills += rs.LegshotKills
			totalHeadshotHit += rs.HeadshotHit
			totalBodyshotHit += rs.BodyshotHit
			totalLegshotHit += rs.LegshotHit

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

			// KAST: rounds with Kill, Assist, Survive, or Traded
			if rs.Kills > 0 || rs.Assists > 0 || rs.Survived || rs.TradedDeath > 0 {
				kastRounds++
			}
		}

		// Get multi-kill stats from time-based detection (not round-based kill count)
		playerMultiKills := AggregateMultiKills(multiKillResults, playerID)
		multiKills := playerMultiKills.MultiKills
		doubleKills := playerMultiKills.DoubleKills
		tripleKills := playerMultiKills.TripleKills
		quadraKills := playerMultiKills.QuadraKills
		pentaKills := playerMultiKills.PentaKills

		roundsPlayed := len(roundStats)

		// Calculate ratios
		var kd, adr, acs, hsPercent, kast float64

		if totalDeaths > 0 {
			kd = float64(totalKills) / float64(totalDeaths)
		}

		if roundsPlayed > 0 {
			adr = float64(totalDamageGiven) / float64(roundsPlayed)
			acs = totalCS / float64(roundsPlayed) // Average Combat Score
			kast = float64(kastRounds) / float64(roundsPlayed) * 100
		}

		// Calculate headshot percent based on hits (aligned with round_player_stats)
		totalHits := totalHeadshotHit + totalBodyshotHit + totalLegshotHit
		if totalHits > 0 {
			hsPercent = float64(totalHeadshotHit) / float64(totalHits) * 100
		}

		// Calculate rounds won by player's team
		var roundsWinRatePercent float64
		var roundsWon int
		playerTeamID, hasTeam := playerTeam[playerID]
		if hasTeam {
			for _, round := range data.Rounds {
				if round.WinnerTeamID != nil && *round.WinnerTeamID == playerTeamID {
					roundsWon++
				}
			}
			if roundsPlayed > 0 {
				roundsWinRatePercent = float64(roundsWon) / float64(roundsPlayed) * 100
			}
		}

		// Calculate first kills/deaths traded
		firstKillsTraded, firstDeathsTraded := computeFirstKillDeathTraded(
			playerID, roundStats, entries, trades,
		)

		// Calculate flawless rounds for this player's team
		flawlessRounds := 0
		if hasTeam {
			flawlessRounds = flawlessRoundsByTeam[playerTeamID]
		}

		// Clutch stats by type
		playerClutches := clutchesByPlayer[playerID]
		clutchesPlayed := len(playerClutches)
		clutchesWon := 0

		v1Played, v1Won := 0, 0
		v2Played, v2Won := 0, 0
		v3Played, v3Won := 0, 0
		v4Played, v4Won := 0, 0
		v5Played, v5Won := 0, 0

		for _, c := range playerClutches {
			if c.Won {
				clutchesWon++
			}

			switch c.Type {
			case 1:
				v1Played++
				if c.Won {
					v1Won++
				}
			case 2:
				v2Played++
				if c.Won {
					v2Won++
				}
			case 3:
				v3Played++
				if c.Won {
					v3Won++
				}
			case 4:
				v4Played++
				if c.Won {
					v4Won++
				}
			case 5:
				v5Played++
				if c.Won {
					v5Won++
				}
			}
		}

		// Determine if this player's team won (compare team tags)
		playerTeamTag := teamTagMap[playerTeamID]
		matchWon := hasTeam && winningTeamTag != "" && (playerTeamTag == winningTeamTag || playerTeamTag == strings.ToUpper(winningTeamTag))

		row := MatchPlayerStatsRow{
			ID:              uuid.New(),
			PlayerID:        playerID,
			MatchID:         &data.MatchID,
			MatchDate:       data.MatchDate,
			ACS:             &acs,
			KD:              &kd,
			KAST:            &kast,
			ADR:             &adr,
			Kills:           totalKills,
			Deaths:          totalDeaths,
			Assists:         totalAssists,
			FirstKills:      totalFirstKills,
			FirstDeaths:     totalFirstDeaths,
			TradeKills:      totalTradeKills,
			TradedDeaths:    totalTradedDeaths,
			Suicides:        totalSuicides,
			TeammatesKilled: totalTeamKills,
			DeathsBySpike:   totalDeathsBySpike,
			ChainKills:      multiKills,
			DoubleKills:     &doubleKills,
			TripleKills:     &tripleKills,
			QuadraKills:     &quadraKills,
			PentaKills:      &pentaKills,
			MultiKills:      doubleKills + tripleKills + quadraKills + pentaKills,
			ClutchesPlayed:  clutchesPlayed,
			ClutchesWon:     clutchesWon,
			V1Played:        &v1Played,
			V1Won:           &v1Won,
			V2Played:        &v2Played,
			V2Won:           &v2Won,
			V3Played:        &v3Played,
			V3Won:           &v3Won,
			V4Played:        &v4Played,
			V4Won:           &v4Won,
			V5Played:        &v5Played,
			V5Won:           &v5Won,
			HeadshotPercent:      &hsPercent,
			HeadshotKills:        &totalHeadshotKills,
			BodyshotKills:        &totalBodyshotKills,
			LegshotKills:         &totalLegshotKills,
			HeadshotHit:          &totalHeadshotHit,
			BodyshotHit:          &totalBodyshotHit,
			LegshotHit:           &totalLegshotHit,
			DamageGiven:          totalDamageGiven,
			DamageTaken:          totalDamageTaken,
			ImpactScore:          new(float64), // Default to 0.0
			MatchesPlayed:        1,
			RoundsPlayed:         roundsPlayed,
			RoundsWinRatePercent: &roundsWinRatePercent,
			IsOvertime:           isOvertime,
			// Phase 5 columns
			RoundsWon:         roundsWon,
			FirstKillsTraded:  firstKillsTraded,
			FirstDeathsTraded: firstDeathsTraded,
			MVP:               0, // Will be set after all players are processed
			FlawlessRounds:    flawlessRounds,
			MatchWon:          matchWon,
			CreatedAt:         now,
		}

		rows = append(rows, row)
	}

	// Compute MVP: highest ACS on winning team (or highest overall if tie)
	computeMVP(rows, playerTeam, teamTagMap, winningTeamTag)

	return rows
}

// computeFlawlessRounds calculates rounds where each team had 0 deaths.
func computeFlawlessRounds(
	rounds []RoundData,
	statsByRound map[uuid.UUID][]RoundPlayerStatsRow,
	playerTeam map[uuid.UUID]uuid.UUID,
) map[uuid.UUID]int {
	result := make(map[uuid.UUID]int)

	for _, round := range rounds {
		roundStats := statsByRound[round.ID]

		// Group deaths by team
		deathsByTeam := make(map[uuid.UUID]int)
		for _, rs := range roundStats {
			teamID, hasTeam := playerTeam[rs.PlayerID]
			if hasTeam {
				deathsByTeam[teamID] += int(rs.Deaths)
			}
		}

		// Count flawless for teams with 0 deaths
		for teamID, deaths := range deathsByTeam {
			if deaths == 0 {
				result[teamID]++
			}
		}
	}

	return result
}

// computeFirstKillDeathTraded calculates first kills and deaths that were traded.
// - FirstKillsTraded: rounds where player got first kill AND then died with that death being traded
// - FirstDeathsTraded: rounds where player got first death AND that death was traded
func computeFirstKillDeathTraded(
	playerID uuid.UUID,
	roundStats []RoundPlayerStatsRow,
	entries map[uuid.UUID]*EntryResult,
	trades map[uuid.UUID]map[uuid.UUID]*TradeResult,
) (firstKillsTraded, firstDeathsTraded int) {
	for _, rs := range roundStats {
		entry := entries[rs.RoundID]
		if entry == nil {
			continue
		}

		roundTrades := trades[rs.RoundID]
		if roundTrades == nil {
			continue
		}

		playerTrade := roundTrades[playerID]

		// FirstKillsTraded: player got first kill AND was subsequently traded (died and teammate avenged)
		if rs.FirstKill && entry.EntryKillerID == playerID {
			// Player got the first kill - check if they died and were traded
			if rs.Deaths > 0 && playerTrade != nil && playerTrade.TradedDeaths > 0 {
				firstKillsTraded++
			}
		}

		// FirstDeathsTraded: player got first death AND that death was traded
		if rs.FirstDeath && entry.EntryVictimID == playerID {
			// Player was the first death - check if their death was traded
			if playerTrade != nil && playerTrade.TradedDeaths > 0 {
				firstDeathsTraded++
			}
		}
	}

	return firstKillsTraded, firstDeathsTraded
}

// computeMVP sets MVP=1 for the player with highest ACS on the winning team.
// If match is tied, MVP goes to player with highest ACS overall.
func computeMVP(rows []MatchPlayerStatsRow, playerTeam map[uuid.UUID]uuid.UUID, teamTagMap map[uuid.UUID]string, winningTeamTag string) {
	if len(rows) == 0 {
		return
	}

	var mvpIndex int
	var highestACS float64 = -1

	for i, row := range rows {
		acs := float64(0)
		if row.ACS != nil {
			acs = *row.ACS
		}

		// If there's a winning team, only consider players on that team
		if winningTeamTag != "" {
			playerTeamID, hasTeam := playerTeam[row.PlayerID]
			if !hasTeam {
				continue
			}
			playerTeamTag := teamTagMap[playerTeamID]
			if playerTeamTag != winningTeamTag && playerTeamTag != strings.ToUpper(winningTeamTag) {
				continue
			}
		}

		if acs > highestACS {
			highestACS = acs
			mvpIndex = i
		}
	}

	// Set MVP for the winner
	if highestACS >= 0 {
		rows[mvpIndex].MVP = 1
	}
}
