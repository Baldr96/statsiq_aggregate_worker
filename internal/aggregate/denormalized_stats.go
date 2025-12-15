package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// BuildPlayerClutchStats creates denormalized player clutch stats rows for CA support.
// Generates one row per player per clutch type (1-5) from match_player_stats v1-v5 columns.
// This replaces the LATERAL unpivot in the PostgreSQL MV.
func BuildPlayerClutchStats(
	matchID uuid.UUID,
	matchDate time.Time,
	matchPlayerStats []MatchPlayerStatsRow,
	now time.Time,
) []PlayerClutchStatsRow {
	var rows []PlayerClutchStatsRow

	for _, mps := range matchPlayerStats {
		// Emit one row per clutch type (1-5) if played > 0
		clutchData := []struct {
			clutchType int16
			played     *int
			won        *int
		}{
			{1, mps.V1Played, mps.V1Won},
			{2, mps.V2Played, mps.V2Won},
			{3, mps.V3Played, mps.V3Won},
			{4, mps.V4Played, mps.V4Won},
			{5, mps.V5Played, mps.V5Won},
		}

		for _, cd := range clutchData {
			played := 0
			if cd.played != nil {
				played = *cd.played
			}
			if played == 0 {
				continue
			}

			won := 0
			if cd.won != nil {
				won = *cd.won
			}

			rows = append(rows, PlayerClutchStatsRow{
				ID:         uuid.New(),
				MatchID:    matchID,
				MatchDate:  matchDate,
				PlayerID:   mps.PlayerID,
				ClutchType: cd.clutchType,
				Played:     played,
				Won:        won,
				CreatedAt:  now,
			})
		}
	}

	return rows
}

// CompositionData holds composition information for a match.
type CompositionData struct {
	MatchID         uuid.UUID
	TeamTag         string
	AgentListHash   string
}

// BuildCompositionWeaponStats creates denormalized composition weapon stats for CA support.
// Aggregates kills by weapon category per composition.
// This replaces the complex round_events JOIN in the PostgreSQL MV.
func BuildCompositionWeaponStats(
	matchID uuid.UUID,
	matchDate time.Time,
	roundEvents []RoundEventData,
	compositions []CompositionData,
	playerTeam map[uuid.UUID]uuid.UUID,
	now time.Time,
) []CompositionWeaponStatsRow {
	// Build team -> composition hash map
	teamToCompHash := make(map[uuid.UUID]string)
	for _, c := range compositions {
		teamID := GetTeamIDByTag(c.TeamTag)
		if teamID != nil {
			teamToCompHash[*teamID] = c.AgentListHash
		}
	}

	// Aggregate kills by composition + weapon category
	type statsKey struct {
		compHash       string
		weaponCategory string
	}
	stats := make(map[statsKey]*CompositionWeaponStatsRow)

	for _, event := range roundEvents {
		if event.EventType != "kill" {
			continue
		}

		// Get player's team
		teamID, ok := playerTeam[event.PlayerID]
		if !ok {
			continue
		}

		// Get composition hash
		compHash, ok := teamToCompHash[teamID]
		if !ok || compHash == "" {
			continue
		}

		// Determine weapon category (default to "Ability")
		weaponCategory := "Ability"
		if event.WeaponCategory != nil && *event.WeaponCategory != "" {
			weaponCategory = *event.WeaponCategory
		}

		key := statsKey{compHash: compHash, weaponCategory: weaponCategory}
		if stats[key] == nil {
			stats[key] = &CompositionWeaponStatsRow{
				ID:              uuid.New(),
				MatchID:         matchID,
				MatchDate:       matchDate,
				CompositionHash: compHash,
				WeaponCategory:  weaponCategory,
				CreatedAt:       now,
			}
		}

		s := stats[key]
		s.TotalKills++

		// Count hit type based on kill flags
		if event.Headshot != nil && *event.Headshot > 0 {
			s.HeadshotKills++
		} else if event.Bodyshot != nil && *event.Bodyshot > 0 {
			s.BodyshotKills++
		} else if event.Legshot != nil && *event.Legshot > 0 {
			s.LegshotKills++
		}

		// Add damage
		if event.DamageGiven != nil {
			s.TotalDamage += *event.DamageGiven
		}
	}

	// Convert map to slice
	var rows []CompositionWeaponStatsRow
	for _, s := range stats {
		rows = append(rows, *s)
	}

	return rows
}

// BuildCompositionClutchStats creates denormalized composition clutch stats for CA support.
// Aggregates clutches by type per composition.
// This replaces the clutches JOIN in the PostgreSQL MV.
func BuildCompositionClutchStats(
	matchID uuid.UUID,
	matchDate time.Time,
	clutchResults []ClutchResult,
	compositions []CompositionData,
	playerTeam map[uuid.UUID]uuid.UUID,
	now time.Time,
) []CompositionClutchStatsRow {
	// Build team -> composition hash map
	teamToCompHash := make(map[uuid.UUID]string)
	for _, c := range compositions {
		teamID := GetTeamIDByTag(c.TeamTag)
		if teamID != nil {
			teamToCompHash[*teamID] = c.AgentListHash
		}
	}

	// Aggregate clutches by composition + type
	type statsKey struct {
		compHash   string
		clutchType int
	}
	stats := make(map[statsKey]*CompositionClutchStatsRow)

	for _, cr := range clutchResults {
		// Get clutcher's team
		teamID, ok := playerTeam[cr.ClutcherID]
		if !ok {
			continue
		}

		// Get composition hash
		compHash, ok := teamToCompHash[teamID]
		if !ok || compHash == "" {
			continue
		}

		// Only include valid clutch types (1-5)
		if cr.Type < 1 || cr.Type > 5 {
			continue
		}

		key := statsKey{compHash: compHash, clutchType: cr.Type}
		if stats[key] == nil {
			stats[key] = &CompositionClutchStatsRow{
				ID:              uuid.New(),
				MatchID:         matchID,
				MatchDate:       matchDate,
				CompositionHash: compHash,
				ClutchType:      int16(cr.Type),
				CreatedAt:       now,
			}
		}

		s := stats[key]
		s.Played++
		if cr.Won {
			s.Won++
		}
	}

	// Convert map to slice
	var rows []CompositionClutchStatsRow
	for _, s := range stats {
		rows = append(rows, *s)
	}

	return rows
}
