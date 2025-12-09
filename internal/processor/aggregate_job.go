package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"worker/internal/aggregate"
	"worker/internal/db"
	"worker/internal/logging"
)

// JobPayload represents the incoming job from the Redis queue.
type JobPayload struct {
	MatchID string `json:"match_id"`
}

// AggregateProcessor handles aggregate computation jobs.
type AggregateProcessor struct {
	ctx    context.Context
	reader *db.CanonicalReader
	writer *db.AggregateWriter
}

// NewAggregateProcessor creates a new aggregate processor.
func NewAggregateProcessor(ctx context.Context, reader *db.CanonicalReader, writer *db.AggregateWriter) *AggregateProcessor {
	return &AggregateProcessor{
		ctx:    ctx,
		reader: reader,
		writer: writer,
	}
}

// Handle processes a single aggregate job from the queue.
func (p *AggregateProcessor) Handle(payload []byte) error {
	logger := logging.Logger()
	startTime := time.Now()

	// Parse job payload
	var job JobPayload
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("unmarshal job payload: %w", err)
	}

	// Parse match ID
	matchID, err := uuid.Parse(job.MatchID)
	if err != nil {
		return fmt.Errorf("parse match_id: %w", err)
	}

	logger.Infof("processing aggregate job for match %s", matchID)

	// Check if match exists
	exists, err := p.reader.MatchExists(p.ctx, matchID)
	if err != nil {
		return fmt.Errorf("check match exists: %w", err)
	}
	if !exists {
		logger.Warnf("match %s not found, skipping", matchID)
		return nil
	}

	// Read canonical data
	data, err := p.reader.GetMatchData(p.ctx, matchID)
	if err != nil {
		return fmt.Errorf("get match data: %w", err)
	}

	logger.Infof("loaded canonical data: %d rounds, %d players, %d events",
		len(data.Rounds), len(data.MatchPlayers), len(data.RoundEvents))

	// Build aggregates
	agg, err := aggregate.BuildAggregates(data)
	if err != nil {
		return fmt.Errorf("build aggregates: %w", err)
	}

	logger.Infof("computed aggregates: %d clutches, %d round_player_stats, %d match_player_stats, %d team_stats, %d side_stats",
		len(agg.Clutches), len(agg.RoundPlayerStats), len(agg.MatchPlayerStats),
		len(agg.TeamMatchStats), len(agg.TeamMatchSideStats))

	// Write to database
	if err := p.writer.WriteAll(p.ctx, agg); err != nil {
		return fmt.Errorf("write aggregates: %w", err)
	}

	elapsed := time.Since(startTime)
	logger.Infof("aggregate job completed for match %s in %v", matchID, elapsed)

	return nil
}
