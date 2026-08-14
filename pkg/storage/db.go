/*
Copyright The FAB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package storage holds the PostgreSQL plumbing shared by the two runtime
// stores: the ontology registry (metadata plane) and the object store (data
// plane).
//
// The two planes are separate concerns and may live in separate databases, so
// nothing here assumes they share a connection.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the subset of pgx a store needs. A pool, a connection and a
// transaction all satisfy it, so the same query code runs inside or outside a
// transaction.
type DB interface {
	// Exec runs a statement that returns no rows.
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	// Query runs a statement that returns rows.
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	// QueryRow runs a statement that returns at most one row.
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

// Beginner is a DB that can start a transaction.
type Beginner interface {
	DB
	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Pool is a connection pool to one PostgreSQL database.
type Pool = pgxpool.Pool

// Open returns a connection pool for the given URL and verifies it is
// reachable, so that a bad DSN fails at startup rather than at first query.
func Open(ctx context.Context, url string) (*Pool, error) {
	if url == "" {
		return nil, errors.New("no PostgreSQL URL configured")
	}

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing PostgreSQL URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	return pool, nil
}

// OpenWait is Open, retrying until the database accepts connections or the
// timeout expires.
//
// A server and its database are started together -- by docker compose locally,
// by an orchestrator elsewhere -- and the database is the slower of the two.
// Waiting turns a guaranteed crash on first boot into a few seconds of startup.
func OpenWait(ctx context.Context, url string, timeout time.Duration) (*Pool, error) {
	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		pool, err := Open(ctx, url)
		if err == nil {
			return pool, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("connecting to PostgreSQL after %s: %w", timeout, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff(attempt)):
		}
	}
}

// retryBackoff grows the wait between connection attempts, capped so that a
// database coming up late is still picked up promptly.
func retryBackoff(attempt int) time.Duration {
	backoff := time.Duration(attempt) * 250 * time.Millisecond
	if backoff > 2*time.Second {
		return 2 * time.Second
	}
	return backoff
}

// InTx runs fn inside a transaction, committing on success and rolling back on
// error or panic.
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			// The rollback error is intentionally dropped: the error that
			// caused the rollback is the one worth reporting.
			_ = tx.Rollback(ctx)
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	committed = true
	return nil
}

// IsUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation, which stores translate into their own conflict errors.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsForeignKeyViolation reports whether err is a PostgreSQL foreign key
// violation.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}
