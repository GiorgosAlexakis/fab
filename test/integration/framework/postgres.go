//go:build integration

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

// Package framework provides the PostgreSQL fixtures the integration tests run
// against.
package framework

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresURLEnvVar names the environment variable holding the connection
// string for the test database.
const PostgresURLEnvVar = "FAB_TEST_POSTGRES_URL"

var schemaCounter atomic.Int64

// NewDatabase returns a connection pool scoped to a PostgreSQL schema created
// for this test and dropped when it finishes.
//
// Every test gets its own schema rather than its own database: the migrations
// address tables unqualified, so a search_path is enough to isolate them, and it
// keeps a full test run to a single database.
func NewDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv(PostgresURLEnvVar)
	if url == "" {
		t.Skipf("set %s to run integration tests", PostgresURLEnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connecting to %s: %v", PostgresURLEnvVar, err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("fab_test_%d_%d", os.Getpid(), schemaCounter.Add(1))
	if _, err := admin.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)); err != nil {
		t.Fatalf("dropping a stale test schema: %v", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("creating the test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parsing %s: %v", PostgresURLEnvVar, err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connecting to the test schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()

		cleanupAdmin, err := pgxpool.New(cleanupCtx, url)
		if err != nil {
			t.Logf("could not connect to drop test schema %s: %v", schema, err)
			return
		}
		defer cleanupAdmin.Close()

		if _, err := cleanupAdmin.Exec(cleanupCtx,
			fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)); err != nil {
			t.Logf("could not drop test schema %s: %v", schema, err)
		}
	})

	return pool
}
