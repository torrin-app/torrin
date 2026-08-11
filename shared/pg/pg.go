package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxPoolConns = 10

func poolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConns > maxPoolConns {
		cfg.MaxConns = maxPoolConns
	}
	return cfg, nil
}

func Open(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
