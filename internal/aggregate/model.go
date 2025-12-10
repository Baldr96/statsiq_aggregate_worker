package aggregate

import (
	"time"

	"github.com/google/uuid"
)

// Global Team IDs for solo queue matches.
var (
	RedTeamID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	BlueTeamID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

// Time constants for game logic.
const (
	TradeWindowMS               = 3000 // Window for trade kill/death detection
	ClutchIdentificationDelayMS = 3000 // Delay to confirm a clutch situation
)

// ClutchRow mirrors the clutches table from init_v4.sql (lines 384-400).
type ClutchRow struct {
	ID                uuid.UUID
	RoundID           uuid.UUID
	PlayerID          uuid.UUID
	Side              *string // "Attack" or "Defense"
	Won               *bool
	IsClutcher        *bool   // true=clutcher, false=opponent
	Situation         *string // "pre-plant" or "post-plant"
	Type              *int    // 1-5 (1vX)
	ClutchStartTimeMS *int
	ClutchEndTimeMS   *int
	CreatedAt         time.Time
}

// RoundPlayerStatsRow mirrors round_player_stats_agregate table (lines 457-488).
type RoundPlayerStatsRow struct {
	ID               uuid.UUID
	RoundID          uuid.UUID
	PlayerID         uuid.UUID
	LoadoutID        *uuid.UUID
	Agent            string
	Rating           float64
	CS               float64 // Combat Score
	Kills            int16
	Deaths           int16
	Assists          int16
	HeadshotPercent  float64
	HeadshotKills    int
	BodyshotKills    int
	LegshotKills     int
	HeadshotHit      int
	BodyshotHit      int
	LegshotHit       int
	DamageGiven      int
	DamageTaken      int
	Survived         bool
	Revived          int
	FirstKill        bool
	FirstDeath       bool
	Suicides         int // Count of suicides this round (not spike deaths)
	DeathsByTeammate int // Count of deaths by teammate this round
	TeammatesKilled  int // Count of teammates killed this round
	KilledBySpike    bool // True if player died to the spike explosion
	TradeKill        int
	TradedDeath      int
	ClutchID         *uuid.UUID
	CreditsSpent     int
	CreditsRemaining int
	IsOvertime       bool // true if round_number >= 24
	CreatedAt        time.Time
}

// MatchPlayerStatsRow mirrors match_player_stats_agregate table (lines 403-454).
type MatchPlayerStatsRow struct {
	ID              uuid.UUID
	PlayerID        uuid.UUID
	MatchID         *uuid.UUID
	Rating          *float64
	ACS             *float64 // Average Combat Score
	KD              *float64 // Kill/Death ratio
	KAST            *float64 // Kill/Assist/Survive/Trade %
	ADR             *float64 // Average Damage per Round
	Kills           int
	Deaths          int
	Assists         int
	FirstKills      int
	FirstDeaths     int
	TradeKills      int
	TradedDeaths    int
	Suicides        int // Total suicides in match
	TeammatesKilled int // Total teammate kills in match
	DeathsBySpike   int // Total deaths by spike explosion
	ChainKills      int // Total chain kills (rounds with 2+ kills)
	DoubleKills     *int // Rounds with exactly 2 kills
	TripleKills     *int // Rounds with exactly 3 kills
	QuadraKills     *int // Rounds with exactly 4 kills
	PentaKills      *int // Rounds with exactly 5 kills
	MultiKills      int  // Sum of double+triple+quadra+penta kills
	ClutchesPlayed  int
	ClutchesWon     int
	V1Played        *int
	V1Won           *int
	V2Played        *int
	V2Won           *int
	V3Played        *int
	V3Won           *int
	V4Played        *int
	V4Won           *int
	V5Played        *int
	V5Won           *int
	HeadshotPercent *float64
	HeadshotKills   *int
	BodyshotKills   *int
	LegshotKills    *int
	HeadshotHit     *int
	BodyshotHit     *int
	LegshotHit      *int
	DamageGiven     int
	DamageTaken     int
	ImpactScore     *float64
	MatchesPlayed   int // Always 1 for per-match stats
	RoundsPlayed    int
	WinRate         *float64 // 0 or 100 (match win/loss)
	IsOvertime      bool     // true if either team has >13 rounds
	CreatedAt       time.Time
}

// TeamMatchStatsRow mirrors team_match_stats_agregate table (lines 498-534).
type TeamMatchStatsRow struct {
	ID                  uuid.UUID
	TeamID              uuid.UUID
	MatchID             uuid.UUID
	MatchType           *string // "Officials", "Scrim", etc.
	RoundsPlayed        int
	RoundsWon           int
	RoundsLost          int
	RoundWinRate        float64
	KD                  float64
	AvgKPR              float64 // Average kills per round
	AvgDPR              float64 // Average deaths per round
	AvgAPR              float64 // Average assists per round
	AvgADR              float64 // Average damage per round
	AvgACS              float64 // Average combat score
	DamageDelta     float64 // Damage given - damage taken
	Kills           int
	Deaths          int
	FirstKills      int
	FirstDeaths     int
	TradeKills      int
	TradedDeaths    int
	Suicides        int // Team total suicides
	TeammatesKilled int // Team total teammate kills
	DeathsBySpike   int // Team total deaths by spike explosion
	ChainKills      int
	DoubleKills     int
	TripleKills     int
	QuadraKills     int
	PentaKills      int
	MultiKills      int  // Sum of double+triple+quadra+penta kills
	ClutchesPlayed      int
	ClutchesWon         int
	ClutchesLoss        int
	ClutchesWR          float64 // Clutch win rate %
	AttackRoundsWin     int
	AttackRoundsPlayed  int
	DefenseRoundsWins   int
	DefenseRoundsPlayed int
	MatchWon            bool // true if team won the match
	IsOvertime          bool // true if either team has >13 rounds
	RoundsOvertimeWon   int  // rounds won in overtime
	RoundsOvertimeLost  int  // rounds lost in overtime
	CreatedAt           time.Time
}

// TeamMatchSideStatsRow mirrors team_match_side_stats_agregate table (lines 537-567).
type TeamMatchSideStatsRow struct {
	ID             uuid.UUID
	TeamID         uuid.UUID
	MatchID        uuid.UUID
	MatchType      *string
	TeamSide       string // "Attack" or "Defense"
	SideOutcome    string // "Win", "Lose", or "Tie"
	RoundsPlayed   int
	RoundsWon      int
	RoundsLost     int
	RoundWinRate   float64
	KD             float64
	AvgKPR         float64
	AvgDPR         float64
	AvgAPR         float64
	AvgADR         float64
	AvgACS         float64
	DamageDelta     float64
	Kills           int
	Deaths          int
	FirstKills      int
	FirstDeaths     int
	TradeKills      int
	TradedDeaths    int
	Suicides        int // Side-specific total suicides
	TeammatesKilled int // Side-specific total teammate kills
	DeathsBySpike   int // Side-specific total deaths by spike explosion
	ChainKills      int
	DoubleKills     int
	TripleKills     int
	QuadraKills     int
	PentaKills      int
	MultiKills      int  // Sum of double+triple+quadra+penta kills
	ClutchesPlayed  int
	ClutchesWon        int
	ClutchesLoss       int
	ClutchesWR         float64
	IsMatchOvertime    bool // true if either team has >13 rounds
	RoundsOvertimeWon  int  // rounds won in overtime for this side
	RoundsOvertimeLost int  // rounds lost in overtime for this side
	CreatedAt          time.Time
}

// AggregateSet groups all computed aggregate data for a match.
type AggregateSet struct {
	MatchID            uuid.UUID
	Clutches           []ClutchRow
	RoundPlayerStats   []RoundPlayerStatsRow
	MatchPlayerStats   []MatchPlayerStatsRow
	TeamMatchStats     []TeamMatchStatsRow
	TeamMatchSideStats []TeamMatchSideStatsRow
}

// TradeResult contains trade detection results for a player in a round.
type TradeResult struct {
	PlayerID     uuid.UUID
	RoundID      uuid.UUID
	TradeKills   int // Kills that are trades (revenge for a teammate)
	TradedDeaths int // Deaths that were traded (teammate took revenge)
}

// EntryResult contains first kill detection result for a round.
type EntryResult struct {
	RoundID       uuid.UUID
	EntryKillerID uuid.UUID
	EntryVictimID uuid.UUID
	TimestampMS   int
}

// ClutchResult contains information about a detected clutch.
type ClutchResult struct {
	RoundID           uuid.UUID
	ClutcherID        uuid.UUID
	OpponentIDs       []uuid.UUID // Opponent players during the clutch
	Type              int         // 1vX
	Won               bool
	Side              string // "Attack" or "Defense"
	Situation         string // "pre-plant" or "post-plant"
	ClutchStartTimeMS int
	ClutchEndTimeMS   int
}

// ClutchState maintains state during clutch detection.
type ClutchState struct {
	Candidate        uuid.UUID   // ID of the last survivor
	AloneSince       int         // Timestamp when they became alone
	OpponentsAtStart int         // Number of opponents at start
	AliveOpponentIDs []uuid.UUID // IDs of opponents alive at clutch start
	Side             string
	Situation        string
	Confirmed        bool
}

// CombatStats holds combat statistics for a player in a round.
type CombatStats struct {
	Kills            int16
	Deaths           int16
	Assists          int16
	HeadshotKills    int
	BodyshotKills    int
	LegshotKills     int
	HeadshotHit      int
	BodyshotHit      int
	LegshotHit       int
	DamageGiven      int
	DamageTaken      int
	Suicides         int // Self-inflicted deaths (not spike)
	TeammatesKilled  int // Kills on teammates
	KilledByTeammate int // Deaths from teammates
	KilledBySpike    int // Deaths from spike explosion
}

// TeamRoundInfo holds round analysis for a team.
type TeamRoundInfo struct {
	Total         int
	Won           int
	AttackPlayed  int
	AttackWon     int
	DefensePlayed int
	DefenseWon    int
}

// GetTeamIDByTag returns the global team UUID for "Red"/"RED" or "Blue"/"BLUE".
func GetTeamIDByTag(tag string) *uuid.UUID {
	switch tag {
	case "Red", "RED":
		return &RedTeamID
	case "Blue", "BLUE":
		return &BlueTeamID
	default:
		return nil
	}
}

// DetermineSide returns the side ("Attack" or "Defense") for a team in a given round.
// - Rounds 0-11 (1st half): RED=Attack, BLUE=Defense
// - Rounds 12-23 (2nd half): RED=Defense, BLUE=Attack
// - Rounds 24+ (Overtime): Alternates every 2 rounds per team
//   OT1 (24-25): RED starts Attack, OT2 (26-27): RED starts Defense, etc.
func DetermineSide(roundNumber int16, teamID uuid.UUID) string {
	isRedTeam := teamID == RedTeamID

	// First half (rounds 0-11)
	if roundNumber < 12 {
		if isRedTeam {
			return "Attack"
		}
		return "Defense"
	}

	// Second half (rounds 12-23)
	if roundNumber < 24 {
		if isRedTeam {
			return "Defense"
		}
		return "Attack"
	}

	// Overtime (rounds 24+)
	// Each OT period is 2 rounds, teams alternate starting side each period
	overtimeRound := roundNumber - 24  // 0, 1, 2, 3, 4, 5...
	otPeriod := overtimeRound / 2      // 0, 0, 1, 1, 2, 2...
	roundInPeriod := overtimeRound % 2 // 0, 1, 0, 1, 0, 1...

	// RED starts Attack in odd OT periods (0, 2, 4...), Defense in even (1, 3, 5...)
	redStartsAttack := otPeriod%2 == 0

	if isRedTeam {
		if redStartsAttack {
			// RED: Attack on first round of period, Defense on second
			if roundInPeriod == 0 {
				return "Attack"
			}
			return "Defense"
		}
		// RED: Defense on first round of period, Attack on second
		if roundInPeriod == 0 {
			return "Defense"
		}
		return "Attack"
	}

	// BLUE team (inverse of RED)
	if redStartsAttack {
		if roundInPeriod == 0 {
			return "Defense"
		}
		return "Attack"
	}
	if roundInPeriod == 0 {
		return "Attack"
	}
	return "Defense"
}

// OtherTeam returns the opposing team ID.
func OtherTeam(teamID uuid.UUID) uuid.UUID {
	if teamID == RedTeamID {
		return BlueTeamID
	}
	return RedTeamID
}

// IsOvertimeRound returns true if the round is in overtime (round_number >= 24).
func IsOvertimeRound(roundNumber int16) bool {
	return roundNumber >= 24
}

// IsMatchOvertime returns true if either team has more than 13 rounds.
func IsMatchOvertime(teamRedScore, teamBlueScore int16) bool {
	return teamRedScore > 13 || teamBlueScore > 13
}
