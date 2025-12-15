package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// WeaponKey is a composite key for tracking weapon stats per player.
// Uses uuid.UUID value type (not pointer) to ensure proper map key comparison.
type WeaponKey struct {
	PlayerID     uuid.UUID
	WeaponID     uuid.UUID // Zero UUID for unknown/ability weapons
	HasWeaponID  bool      // True if weapon_id was present
	WeaponName   string
}

// WeaponStats tracks statistics for a specific weapon used by a player.
type WeaponStats struct {
	WeaponCategory *string
	Kills          int
	Deaths         int // Deaths caused by this weapon (from victim's perspective)
	DamageGiven    int
	DamageTaken    int // Damage taken from this weapon (from victim's perspective)
	FirstKills     int
	HeadshotKills  int
	BodyshotKills  int
	LegshotKills   int
}

// ComputeWeaponStats calculates weapon-specific statistics for each player.
// Returns a map from WeaponKey to WeaponStats for aggregation into match_player_weapon_stats_agregate.
func ComputeWeaponStats(
	events []RoundEventData,
	entries map[uuid.UUID]*EntryResult,
	playerTeam map[uuid.UUID]uuid.UUID,
) map[WeaponKey]*WeaponStats {
	weaponStats := make(map[WeaponKey]*WeaponStats)

	// Helper to get weapon name with fallback
	getWeaponName := func(e RoundEventData) string {
		if e.Weapon != nil && *e.Weapon != "" {
			return *e.Weapon
		}
		return "Unknown"
	}

	getOrCreate := func(playerID uuid.UUID, weaponID *uuid.UUID, weaponName string, category *string) *WeaponStats {
		var wid uuid.UUID
		hasWid := false
		if weaponID != nil {
			wid = *weaponID
			hasWid = true
		}
		key := WeaponKey{PlayerID: playerID, WeaponID: wid, HasWeaponID: hasWid, WeaponName: weaponName}
		if weaponStats[key] == nil {
			weaponStats[key] = &WeaponStats{WeaponCategory: category}
		}
		return weaponStats[key]
	}

	// Build entry events map for first kill detection
	entryEvents := make(map[uuid.UUID]*EntryResult)
	for roundID, entry := range entries {
		entryEvents[roundID] = entry
	}

	for _, e := range events {
		weaponName := getWeaponName(e)

		// Skip Spike deaths for weapon stats (not really a weapon)
		if weaponName == "Spike" {
			continue
		}

		switch e.EventType {
		case "kill":
			if e.VictimID == nil {
				continue
			}
			victimID := *e.VictimID

			// Skip self-kills
			if e.PlayerID == victimID {
				continue
			}

			// Skip teamkills
			killerTeam, killerHasTeam := playerTeam[e.PlayerID]
			victimTeam, victimHasTeam := playerTeam[victimID]
			if killerHasTeam && victimHasTeam && killerTeam == victimTeam {
				continue
			}

			// Player got a kill with this weapon
			killerStats := getOrCreate(e.PlayerID, e.WeaponID, weaponName, e.WeaponCategory)
			killerStats.Kills++

			// Determine kill type
			if e.Headshot != nil && *e.Headshot > 0 {
				killerStats.HeadshotKills++
			} else if e.Bodyshot != nil && *e.Bodyshot > 0 {
				killerStats.BodyshotKills++
			} else if e.Legshot != nil && *e.Legshot > 0 {
				killerStats.LegshotKills++
			}

			// Check if this was an entry kill
			if entry, ok := entryEvents[e.RoundID]; ok {
				if entry.EntryKillerID == e.PlayerID && entry.EntryVictimID == victimID {
					killerStats.FirstKills++
				}
			}

			// Victim died to this weapon
			victimStats := getOrCreate(victimID, e.WeaponID, weaponName, e.WeaponCategory)
			victimStats.Deaths++

		case "damage":
			if e.VictimID == nil || e.DamageGiven == nil {
				continue
			}
			victimID := *e.VictimID

			// Skip self-damage
			if e.PlayerID == victimID {
				continue
			}

			// Attacker dealt damage with this weapon
			attackerStats := getOrCreate(e.PlayerID, e.WeaponID, weaponName, e.WeaponCategory)
			attackerStats.DamageGiven += *e.DamageGiven

			// Victim took damage from this weapon
			victimStats := getOrCreate(victimID, e.WeaponID, weaponName, e.WeaponCategory)
			victimStats.DamageTaken += *e.DamageGiven
		}
	}

	return weaponStats
}

// BuildMatchPlayerWeaponStats converts computed weapon statistics into database rows.
func BuildMatchPlayerWeaponStats(
	matchID uuid.UUID,
	matchDate time.Time,
	weaponStats map[WeaponKey]*WeaponStats,
	now time.Time,
) []MatchPlayerWeaponStatsRow {
	var rows []MatchPlayerWeaponStatsRow

	for key, stats := range weaponStats {
		// Only include weapons with actual interactions
		if stats.Kills == 0 && stats.Deaths == 0 && stats.DamageGiven == 0 && stats.DamageTaken == 0 {
			continue
		}

		// Convert WeaponID back to pointer for database row
		var weaponIDPtr *uuid.UUID
		if key.HasWeaponID {
			wid := key.WeaponID
			weaponIDPtr = &wid
		}

		rows = append(rows, MatchPlayerWeaponStatsRow{
			ID:             uuid.New(),
			MatchID:        matchID,
			MatchDate:      matchDate,
			PlayerID:       key.PlayerID,
			WeaponID:       weaponIDPtr,
			WeaponName:     key.WeaponName,
			WeaponCategory: stats.WeaponCategory,
			Kills:          stats.Kills,
			Deaths:         stats.Deaths,
			DamageGiven:    stats.DamageGiven,
			DamageTaken:    stats.DamageTaken,
			FirstKills:     stats.FirstKills,
			HeadshotKills:  stats.HeadshotKills,
			BodyshotKills:  stats.BodyshotKills,
			LegshotKills:   stats.LegshotKills,
			CreatedAt:      now,
		})
	}

	return rows
}
