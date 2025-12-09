package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildMatchPlayerStats aggregates round stats into match-level player statistics.
func BuildMatchPlayerStats(
	data *MatchData,
	roundPlayerStats []RoundPlayerStatsRow,
	clutches []ClutchResult,
	now time.Time,
) []MatchPlayerStatsRow {
	// Calculate is_overtime at match level
	isOvertime := IsMatchOvertime(data.TeamRedScore, data.TeamBlueScore)

	// Group round stats by player
	statsByPlayer := make(map[uuid.UUID][]RoundPlayerStatsRow)
	for _, rps := range roundPlayerStats {
		statsByPlayer[rps.PlayerID] = append(statsByPlayer[rps.PlayerID], rps)
	}

	// Group clutches by player (clutcher only)
	clutchesByPlayer := make(map[uuid.UUID][]ClutchResult)
	for _, c := range clutches {
		clutchesByPlayer[c.ClutcherID] = append(clutchesByPlayer[c.ClutcherID], c)
	}

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

		doubleKills := 0
		tripleKills := 0
		quadraKills := 0
		pentaKills := 0
		multiKills := 0
		kastRounds := 0

		for _, rs := range roundStats {
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
			if rs.Suicide {
				totalSuicides++
			}
			if rs.KilledBySpike {
				totalDeathsBySpike++
			}
			totalTeamKills += rs.KilledTeammate

			// Count multi-kills by round
			kills := int(rs.Kills)
			if kills == 2 {
				doubleKills++
			} else if kills == 3 {
				tripleKills++
			} else if kills == 4 {
				quadraKills++
			} else if kills >= 5 {
				pentaKills++
			}
			if kills >= 2 {
				multiKills++
			}

			// KAST: rounds with Kill, Assist, Survive, or Traded
			if rs.Kills > 0 || rs.Assists > 0 || rs.Survived || rs.TradedDeath > 0 {
				kastRounds++
			}
		}

		roundsPlayed := len(roundStats)

		// Calculate ratios
		var kd, adr, hsPercent, kast float64

		if totalDeaths > 0 {
			kd = float64(totalKills) / float64(totalDeaths)
		}

		if roundsPlayed > 0 {
			adr = float64(totalDamageGiven) / float64(roundsPlayed)
			kast = float64(kastRounds) / float64(roundsPlayed) * 100
		}

		if totalKills > 0 {
			hsPercent = float64(totalHeadshotKills) / float64(totalKills) * 100
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

		row := MatchPlayerStatsRow{
			ID:              uuid.New(),
			PlayerID:        playerID,
			MatchID:         &data.MatchID,
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
			TeamKills:       totalTeamKills,
			DeathsBySpike:   totalDeathsBySpike,
			MultiKills:      multiKills,
			DoubleKills:     &doubleKills,
			TripleKills:     &tripleKills,
			QuadraKills:     &quadraKills,
			PentaKills:      &pentaKills,
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
			HeadshotPercent: &hsPercent,
			HeadshotKills:   &totalHeadshotKills,
			BodyshotKills:   &totalBodyshotKills,
			LegshotKills:    &totalLegshotKills,
			HeadshotHit:     &totalHeadshotHit,
			BodyshotHit:     &totalBodyshotHit,
			LegshotHit:      &totalLegshotHit,
			DamageGiven:     totalDamageGiven,
			DamageTaken:     totalDamageTaken,
			MatchesPlayed:   1,
			RoundsPlayed:    roundsPlayed,
			IsOvertime:      isOvertime,
			CreatedAt:       now,
		}

		rows = append(rows, row)
	}

	return rows
}
