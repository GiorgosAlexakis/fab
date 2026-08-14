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

package registry

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	"github.com/GiorgosAlexakis/fab/pkg/storage/migrate"
)

const migrateExample = `  # Create or update the registry schema
  fab registry migrate --registry-url postgres://localhost/fab

  # Show what would be applied
  fab registry migrate --dry-run`

// MigrateOptions is the configuration of a `fab registry migrate` invocation.
type MigrateOptions struct {
	genericiooptions.IOStreams

	// DryRun lists pending migrations without applying them.
	DryRun bool

	factory cmdutil.Factory
}

// NewMigrateOptions returns MigrateOptions with defaults.
func NewMigrateOptions(streams genericiooptions.IOStreams) *MigrateOptions {
	return &MigrateOptions{IOStreams: streams}
}

// NewCmdMigrate returns the `fab registry migrate` command.
func NewCmdMigrate(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewMigrateOptions(streams)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply the registry schema migrations",
		Long: "Create or update the ontology registry schema.\n\n" +
			"These migrations are platform infrastructure shipped with fab, not the\n" +
			"per-object-type migrations generated from an ontology. They are idempotent:\n" +
			"running this command against an up-to-date registry does nothing.",
		Example: migrateExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "List pending migrations without applying them.")

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *MigrateOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}
	o.factory = f
	return nil
}

// Run applies or lists the migrations.
func (o *MigrateOptions) Run(ctx context.Context) error {
	db, err := o.factory.RegistryDB(ctx)
	if err != nil {
		return err
	}

	migrations, err := registrypostgres.Migrations()
	if err != nil {
		return err
	}

	if o.DryRun {
		pending, err := migrate.Pending(ctx, db, registrypostgres.Component, migrations)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			fmt.Fprintln(o.Out, "The registry schema is up to date.")
			return nil
		}
		for _, migration := range pending {
			fmt.Fprintf(o.Out, "pending %04d_%s\n", migration.Version, migration.Name)
		}
		return nil
	}

	applied, err := migrate.Apply(ctx, db, registrypostgres.Component, migrations)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(o.Out, "The registry schema is up to date.")
		return nil
	}
	for _, migration := range applied {
		fmt.Fprintf(o.Out, "applied %04d_%s\n", migration.Version, migration.Name)
	}
	return nil
}
