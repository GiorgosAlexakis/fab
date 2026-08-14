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

// Package app builds and runs the object store server.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	objectstorepostgres "github.com/GiorgosAlexakis/fab/pkg/objectstore/postgres"
	objectstoreserver "github.com/GiorgosAlexakis/fab/pkg/objectstore/server"
	registryclient "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/client"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
	"github.com/GiorgosAlexakis/fab/pkg/version"
)

// Environment variables the server reads when the matching flag is unset.
const (
	// ListenAddressEnvVar holds the address to listen on.
	ListenAddressEnvVar = "FAB_LISTEN_ADDRESS"
	// DatabaseURLEnvVar holds the PostgreSQL URL of the object store database.
	DatabaseURLEnvVar = "FAB_OBJECT_STORE_DB_URL"
	// RegistryURLEnvVar holds the base URL of the ontology registry server.
	RegistryURLEnvVar = "FAB_REGISTRY_URL"
	// OntologyNameEnvVar holds the ontology to bind to.
	OntologyNameEnvVar = "FAB_ONTOLOGY_NAME"
	// OntologyTagEnvVar holds the environment tag to bind to.
	OntologyTagEnvVar = "FAB_ONTOLOGY_TAG"
	// LogLevelEnvVar holds the log level.
	LogLevelEnvVar = "FAB_LOG_LEVEL"
)

// Defaults for a local deployment, where both servers run beside each other.
const (
	// DefaultListenAddress is the port the object store serves on.
	DefaultListenAddress = ":8082"
	// DefaultRegistryURL is where the registry server listens.
	DefaultRegistryURL = "http://localhost:8081"
	// DefaultOntologyTag is the environment tag to bind to.
	DefaultOntologyTag = "dev"
)

// DatabaseWaitTimeout bounds how long startup waits for the database.
const DatabaseWaitTimeout = 60 * time.Second

// Options is the configuration of the object store server.
type Options struct {
	// ListenAddress is the host:port to serve on.
	ListenAddress string
	// DatabaseURL is the PostgreSQL URL of the object store database.
	DatabaseURL string
	// RegistryURL is the base URL of the ontology registry server.
	RegistryURL string
	// OntologyName is the ontology whose versions the store binds to.
	OntologyName string
	// OntologyTag is the environment tag requests are served against unless
	// they select a version of their own.
	OntologyTag string
	// BindingTTL is how long a resolved ontology version is reused.
	BindingTTL time.Duration
	// SkipMigrations starts the server without bringing the schema up to date.
	SkipMigrations bool
	// ShutdownTimeout bounds draining in-flight requests on shutdown.
	ShutdownTimeout time.Duration
	// LogLevel is one of debug, info, warn or error.
	LogLevel string
}

// NewCommand returns the ontology-objectstore command.
func NewCommand() *cobra.Command {
	o := &Options{}

	cmd := &cobra.Command{
		Use:   "ontology-objectstore",
		Short: "Serve the object store",
		Long: "The object store is the data plane: it holds object instances, their current\n" +
			"property values and the links between them.\n\n" +
			"It is generic over the ontology. Requests are served against the ontology\n" +
			"version an environment tag points at, resolved from the registry, so publishing\n" +
			"a new version changes what the store accepts without migrating any data.",
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
		fmt.Sprintf("PostgreSQL URL of the object store database. Defaults to $%s.", DatabaseURLEnvVar))
	flags.StringVar(&o.RegistryURL, "registry-url", o.RegistryURL,
		fmt.Sprintf("Base URL of the ontology registry server. Defaults to $%s, else %s.",
			RegistryURLEnvVar, DefaultRegistryURL))
	flags.StringVar(&o.OntologyName, "ontology-name", o.OntologyName,
		fmt.Sprintf("Ontology to bind to. Defaults to $%s.", OntologyNameEnvVar))
	flags.StringVar(&o.OntologyTag, "ontology-tag", o.OntologyTag,
		fmt.Sprintf("Environment tag to bind to. Defaults to $%s, else %s.",
			OntologyTagEnvVar, DefaultOntologyTag))
	flags.DurationVar(&o.BindingTTL, "ontology-refresh-interval", objectstoreserver.DefaultBindingTTL,
		"How long a resolved ontology version is reused before the registry is asked again.")
	flags.BoolVar(&o.SkipMigrations, "skip-migrations", o.SkipMigrations,
		"Serve without applying the object store migrations. The schema must already be current.")
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
	o.RegistryURL = apiserver.EnvOr(o.RegistryURL, RegistryURLEnvVar, DefaultRegistryURL)
	o.OntologyName = apiserver.EnvOr(o.OntologyName, OntologyNameEnvVar, "")
	o.OntologyTag = apiserver.EnvOr(o.OntologyTag, OntologyTagEnvVar, DefaultOntologyTag)
	o.LogLevel = apiserver.EnvOr(o.LogLevel, LogLevelEnvVar, "info")
}

// Validate checks the options before anything is opened.
func (o *Options) Validate() error {
	if o.DatabaseURL == "" {
		return fmt.Errorf("no object store database: pass --database-url or set $%s", DatabaseURLEnvVar)
	}
	if o.OntologyName == "" {
		return fmt.Errorf("no ontology: pass --ontology-name or set $%s", OntologyNameEnvVar)
	}
	if o.ListenAddress == "" {
		return errors.New("--listen must not be empty")
	}
	return nil
}

// Run opens the database, brings the schema up to date and serves until the
// context is cancelled.
//
// The ontology is deliberately not resolved at startup: the store is useful
// before anything has been published, and a registry that is briefly unreachable
// should make requests fail, not keep the process from starting.
func (o *Options) Run(ctx context.Context) error {
	logger, err := apiserver.NewLogger(o.LogLevel, os.Stderr)
	if err != nil {
		return err
	}
	logger = logger.With("server", "ontology-objectstore")

	pool, err := storage.OpenWait(ctx, o.DatabaseURL, DatabaseWaitTimeout)
	if err != nil {
		return err
	}
	defer pool.Close()

	if !o.SkipMigrations {
		applied, err := objectstorepostgres.Migrate(ctx, pool)
		if err != nil {
			return fmt.Errorf("applying the object store migrations: %w", err)
		}
		for _, migration := range applied {
			logger.Info("applied migration", "version", migration.Version, "name", migration.Name)
		}
	}

	client, err := registryclient.New(o.RegistryURL)
	if err != nil {
		return err
	}
	resolver, err := objectstoreserver.NewPostgresResolver(pool, client,
		o.OntologyName, o.OntologyTag, o.BindingTTL)
	if err != nil {
		return err
	}
	logger.Info("bound to ontology", "name", o.OntologyName, "tag", o.OntologyTag, "registry", o.RegistryURL)

	handler := apiserver.WithLogging(objectstoreserver.New(resolver, pool.Ping), logger)

	return apiserver.Serve(ctx, handler, apiserver.Options{
		Address:         o.ListenAddress,
		ShutdownTimeout: o.ShutdownTimeout,
	}, logger)
}
