package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildRoundPlayerStats constructs round-level statistics for each player.
func BuildRoundPlayerStats(
	data *MatchData,
	trades map[uuid.UUID]map[uuid.UUID]*TradeResult,
	entries map[uuid.UUID]*EntryResult,
	clutches []ClutchResult,
	playerTeam map[uuid.UUID]uuid.UUID,
	now time.Time,
) []RoundPlayerStatsRow {
	// Build helper maps
	playerAgent := buildPlayerAgentMap(data.MatchPlayers)
	clutchByRoundPlayer := buildClutchByRoundPlayerMap(clutches)
	eventsByRound := groupEventsByRound(data.RoundEvents)
	loadoutsByRPS := buildLoadoutMap(data.RoundPlayerLoadouts, data.RoundPlayerStates)
	rpsByRound := groupRPSByRound(data.RoundPlayerStates)

	var rows []RoundPlayerStatsRow

	for _, round := range data.Rounds {
		events := eventsByRound[round.ID]
		roundPlayerStates := rpsByRound[round.ID]

		// Compute combat stats for this round (including suicide/teamkill detection)
		combatStats := computeRoundCombatStats(events, playerTeam)

		for _, rps := range roundPlayerStates {
			stats := combatStats[rps.PlayerID]
			if stats == nil {
				stats = &CombatStats{}
			}

			// Get trade info for this round
			var tradeKills, tradedDeaths int
			if roundTrades, ok := trades[round.ID]; ok {
				if tradeInfo, ok := roundTrades[rps.PlayerID]; ok {
					tradeKills = tradeInfo.TradeKills
					tradedDeaths = tradeInfo.TradedDeaths
				}
			}

			// Check first kill/death
			var firstKill, firstDeath bool
			if entry, ok := entries[round.ID]; ok {
				firstKill = entry.EntryKillerID == rps.PlayerID
				firstDeath = entry.EntryVictimID == rps.PlayerID
			}

			// Check clutch
			var clutchID *uuid.UUID
			if clutch, ok := clutchByRoundPlayer[round.ID]; ok {
				if clutch.PlayerID == rps.PlayerID {
					clutchID = &clutch.ClutchID
				}
			}

			// Calculate headshot percent based on hits
			totalHits := stats.HeadshotHit + stats.BodyshotHit + stats.LegshotHit
			var hsPercent float64
			if totalHits > 0 {
				hsPercent = float64(stats.HeadshotHit) / float64(totalHits) * 100
			}

			// Get loadout data
			loadout := loadoutsByRPS[rps.ID]
			var loadoutID *uuid.UUID
			var creditsSpent, creditsRemaining int
			if loadout != nil {
				loadoutID = loadout.LoadoutID
				if loadout.Spent != nil {
					creditsSpent = *loadout.Spent
				}
				if loadout.Remaining != nil {
					creditsRemaining = *loadout.Remaining
				}
			}

			// Get Combat Score from round_player_state.score
			var cs float64
			if rps.Score != nil {
				cs = float64(*rps.Score)
			}

			rows = append(rows, RoundPlayerStatsRow{
				ID:               uuid.New(),
				RoundID:          round.ID,
				PlayerID:         rps.PlayerID,
				LoadoutID:        loadoutID,
				Agent:            playerAgent[rps.PlayerID],
				Rating:           0, // TODO: Calculate rating
				CS:               cs,
				Kills:            stats.Kills,
				Deaths:           stats.Deaths,
				Assists:          stats.Assists,
				HeadshotPercent:  hsPercent,
				HeadshotKills:    stats.HeadshotKills,
				BodyshotKills:    stats.BodyshotKills,
				LegshotKills:     stats.LegshotKills,
				HeadshotHit:      stats.HeadshotHit,
				BodyshotHit:      stats.BodyshotHit,
				LegshotHit:       stats.LegshotHit,
				DamageGiven:      stats.DamageGiven,
				DamageTaken:      stats.DamageTaken,
				Survived:         stats.Deaths == 0,
				Revived:          0,
				FirstKill:        firstKill,
				FirstDeath:       firstDeath,
				Suicide:          stats.Suicides > 0,
				KilledByTeammate: stats.KilledByTeammate > 0,
				KilledTeammate:   stats.TeamKills,
				KilledBySpike:    stats.KilledBySpike > 0,
				TradeKill:        tradeKills,
				TradedDeath:      tradedDeaths,
				ClutchID:         clutchID,
				CreditsSpent:     creditsSpent,
				CreditsRemaining: creditsRemaining,
				IsOvertime:       IsOvertimeRound(round.RoundNumber),
				CreatedAt:        now,
			})
		}
	}

	return rows
}

// buildPlayerAgentMap creates a mapping from player ID to agent name.
func buildPlayerAgentMap(matchPlayers []MatchPlayerData) map[uuid.UUID]string {
	m := make(map[uuid.UUID]string)
	for _, mp := range matchPlayers {
		m[mp.PlayerID] = mp.AgentName
	}
	return m
}

// clutchByRoundPlayerEntry holds clutch info for lookup.
type clutchByRoundPlayerEntry struct {
	ClutchID uuid.UUID
	PlayerID uuid.UUID
}

// buildClutchByRoundPlayerMap creates a lookup for clutches by round.
func buildClutchByRoundPlayerMap(clutches []ClutchResult) map[uuid.UUID]*clutchByRoundPlayerEntry {
	m := make(map[uuid.UUID]*clutchByRoundPlayerEntry)
	for _, c := range clutches {
		clutchID := uuid.New()
		m[c.RoundID] = &clutchByRoundPlayerEntry{
			ClutchID: clutchID,
			PlayerID: c.ClutcherID,
		}
	}
	return m
}

// groupEventsByRound groups events by round ID.
func groupEventsByRound(events []RoundEventData) map[uuid.UUID][]RoundEventData {
	m := make(map[uuid.UUID][]RoundEventData)
	for _, e := range events {
		m[e.RoundID] = append(m[e.RoundID], e)
	}
	return m
}

// buildLoadoutMap creates a lookup from round_player_state ID to loadout data.
func buildLoadoutMap(loadouts []RoundPlayerLoadoutData, states []RoundPlayerStateData) map[uuid.UUID]*RoundPlayerLoadoutData {
	m := make(map[uuid.UUID]*RoundPlayerLoadoutData)
	for i := range loadouts {
		m[loadouts[i].RoundPlayerID] = &loadouts[i]
	}
	return m
}

// groupRPSByRound groups round player states by round ID.
func groupRPSByRound(states []RoundPlayerStateData) map[uuid.UUID][]RoundPlayerStateData {
	m := make(map[uuid.UUID][]RoundPlayerStateData)
	for _, s := range states {
		m[s.RoundID] = append(m[s.RoundID], s)
	}
	return m
}

// computeRoundCombatStats calculates combat statistics from events.
// playerTeam is used to detect teamkills (killer and victim on same team).
func computeRoundCombatStats(events []RoundEventData, playerTeam map[uuid.UUID]uuid.UUID) map[uuid.UUID]*CombatStats {
	stats := make(map[uuid.UUID]*CombatStats)

	getOrCreate := func(id uuid.UUID) *CombatStats {
		if stats[id] == nil {
			stats[id] = &CombatStats{}
		}
		return stats[id]
	}

	for _, e := range events {
		switch e.EventType {
		case "kill":
			// Check if this is a self-kill (killer == victim)
			isSelfKill := e.VictimID != nil && e.PlayerID == *e.VictimID

			// Check if self-kill is from spike explosion
			isSpikeDeath := isSelfKill && e.Weapon != nil && *e.Weapon == "Spike"

			// Check if this is a teamkill (same team, different player)
			isTeamkill := false
			if e.VictimID != nil && !isSelfKill {
				killerTeam, killerHasTeam := playerTeam[e.PlayerID]
				victimTeam, victimHasTeam := playerTeam[*e.VictimID]
				if killerHasTeam && victimHasTeam && killerTeam == victimTeam {
					isTeamkill = true
				}
			}

			if isSpikeDeath {
				// Spike death: track separately, no kill credit, not a suicide
				victim := getOrCreate(*e.VictimID)
				victim.Deaths++
				victim.KilledBySpike++
			} else if isSelfKill {
				// True suicide: track as suicide, no kill credit
				victim := getOrCreate(*e.VictimID)
				victim.Deaths++
				victim.Suicides++
			} else if isTeamkill {
				// Teamkill: track separately, no kill credit
				killer := getOrCreate(e.PlayerID)
				killer.TeamKills++
				victim := getOrCreate(*e.VictimID)
				victim.Deaths++
				victim.KilledByTeammate++
			} else {
				// Normal kill
				killer := getOrCreate(e.PlayerID)
				killer.Kills++

				// Determine kill type based on headshot/bodyshot/legshot
				if e.Headshot != nil && *e.Headshot > 0 {
					killer.HeadshotKills++
				} else if e.Bodyshot != nil && *e.Bodyshot > 0 {
					killer.BodyshotKills++
				} else if e.Legshot != nil && *e.Legshot > 0 {
					killer.LegshotKills++
				}

				// Victim gets a death
				if e.VictimID != nil {
					victim := getOrCreate(*e.VictimID)
					victim.Deaths++
				}
			}

		case "damage":
			// Damage given
			attacker := getOrCreate(e.PlayerID)
			if e.DamageGiven != nil {
				attacker.DamageGiven += *e.DamageGiven
			}
			if e.Headshot != nil {
				attacker.HeadshotHit += *e.Headshot
			}
			if e.Bodyshot != nil {
				attacker.BodyshotHit += *e.Bodyshot
			}
			if e.Legshot != nil {
				attacker.LegshotHit += *e.Legshot
			}

			// Damage taken
			if e.VictimID != nil && e.DamageGiven != nil {
				victim := getOrCreate(*e.VictimID)
				victim.DamageTaken += *e.DamageGiven
			}
		}
	}

	return stats
}
