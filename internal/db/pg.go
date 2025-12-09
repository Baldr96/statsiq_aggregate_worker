package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool configures a pgx connection pool for the aggregate worker service.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}
