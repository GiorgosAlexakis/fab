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

// Package app builds and runs the ontology registry server.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	registryserver "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/server"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
	"github.com/GiorgosAlexakis/fab/pkg/version"
)

// Environment variables the server reads when the matching flag is unset, so
// that a container is configured without a command line.
const (
	// ListenAddressEnvVar holds the address to listen on.
	ListenAddressEnvVar = "FAB_LISTEN_ADDRESS"
	// DatabaseURLEnvVar holds the PostgreSQL URL of the registry database.
	DatabaseURLEnvVar = "FAB_REGISTRY_DB_URL"
	// LogLevelEnvVar holds the log level.
	LogLevelEnvVar = "FAB_LOG_LEVEL"
)

// DefaultListenAddress is the port the registry serves on.
const DefaultListenAddress = ":8081"

// DatabaseWaitTimeout bounds how long startup waits for the database.
const DatabaseWaitTimeout = 60 * time.Second

// Options is the configuration of the registry server.
type Options struct {
	// ListenAddress is the host:port to serve on.
	ListenAddress string
	// DatabaseURL is the PostgreSQL URL of the registry database.
	DatabaseURL string
	// SkipMigrations starts the server without bringing the schema up to date.
	SkipMigrations bool
	// ShutdownTimeout bounds draining in-flight requests on shutdown.
	ShutdownTimeout time.Duration
	// LogLevel is one of debug, info, warn or error.
	LogLevel string
}

// NewCommand returns the ontology-registry command.
func NewCommand() *cobra.Command {
	o := &Options{}

	cmd := &cobra.Command{
		Use:   "ontology-registry",
		Short: "Serve the ontology registry",
		Long: "The ontology registry is the metadata plane: it stores versioned ontology\n" +
			"snapshots and the environment tags that point at them.\n\n" +
			"It owns its database schema and brings it up to date on startup, so the only\n" +
			"thing a deployment has to provide is a PostgreSQL URL.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Get().GitVersion,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Complete()
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&o.ListenAddress, "listen", o.ListenAddress,
		fmt.Sprintf("Address to serve on. Defaults to $%s, else %s.", ListenAddressEnvVar, DefaultListenAddress))
	flags.StringVar(&o.DatabaseURL, "database-url", o.DatabaseURL,
		fmt.Sprintf("PostgreSQL URL of the registry database. Defaults to $%s.", DatabaseURLEnvVar))
	flags.BoolVar(&o.SkipMigrations, "skip-migrations", o.SkipMigrations,
		"Serve without applying the registry migrations. The schema must already be current.")
	flags.DurationVar(&o.ShutdownTimeout, "shutdown-timeout", apiserver.DefaultShutdownTimeout,
		"How long in-flight requests get to finish after a shutdown signal.")
	flags.StringVar(&o.LogLevel, "log-level", o.LogLevel,
		fmt.Sprintf("Log level: debug, info, warn or error. Defaults to $%s, else info.", LogLevelEnvVar))

	return cmd
}

// Complete fills in what the flags left unset from the environment.
func (o *Options) Complete() {
	o.ListenAddress = apiserver.EnvOr(o.ListenAddress, ListenAddressEnvVar, DefaultListenAddress)
	o.DatabaseURL = apiserver.EnvOr(o.DatabaseURL, DatabaseURLEnvVar, "")
	o.LogLevel = apiserver.EnvOr(o.LogLevel, LogLevelEnvVar, "info")
}

// Validate checks the options before anything is opened.
func (o *Options) Validate() error {
	if o.DatabaseURL == "" {
		return fmt.Errorf("no registry database: pass --database-url or set $%s", DatabaseURLEnvVar)
	}
	if o.ListenAddress == "" {
		return errors.New("--listen must not be empty")
	}
	return nil
}

// Run opens the database, brings the schema up to date and serves until the
// context is cancelled.
func (o *Options) Run(ctx context.Context) error {
	logger, err := apiserver.NewLogger(o.LogLevel, os.Stderr)
	if err != nil {
		return err
	}
	logger = logger.With("server", "ontology-registry")

	pool, err := storage.OpenWait(ctx, o.DatabaseURL, DatabaseWaitTimeout)
	if err != nil {
		return err
	}
	defer pool.Close()

	if !o.SkipMigrations {
		applied, err := registrypostgres.Migrate(ctx, pool)
		if err != nil {
			return fmt.Errorf("applying the registry migrations: %w", err)
		}
		for _, migration := range applied {
			logger.Info("applied migration", "version", migration.Version, "name", migration.Name)
		}
	}

	store := registrypostgres.New(pool)
	handler := apiserver.WithLogging(registryserver.New(store, pool.Ping), logger)

	return apiserver.Serve(ctx, handler, apiserver.Options{
		Address:         o.ListenAddress,
		ShutdownTimeout: o.ShutdownTimeout,
	}, logger)
}
