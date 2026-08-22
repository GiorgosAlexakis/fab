// Package genericclioptions holds the flags that are common to every fab
// command, which today is one question: where the foundry is.
package genericclioptions

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
)

const (
	// FoundryConfigFile marks the root of a foundry.
	FoundryConfigFile = "foundry.yaml"
	// FoundryRootEnvVar overrides foundry discovery.
	FoundryRootEnvVar = "FAB_ROOT"
)

// ConfigFlags composes the flags every command shares. Commands never read these
// fields directly; they go through a Factory.
type ConfigFlags struct {
	// Root is the foundry root. Empty means "discover it".
	Root *string
}

// NewConfigFlags returns ConfigFlags with the standard foundry layout.
func NewConfigFlags() *ConfigFlags {
	return &ConfigFlags{Root: stringptr("")}
}

// AddFlags binds the foundry flags to the given flag set.
func (f *ConfigFlags) AddFlags(flags *pflag.FlagSet) {
	if f.Root != nil {
		flags.StringVar(f.Root, "root", *f.Root,
			fmt.Sprintf("Path to the foundry root. Defaults to $%s, else the nearest ancestor directory containing %s.",
				FoundryRootEnvVar, FoundryConfigFile))
	}
}

// FoundryRoot resolves the foundry root, in precedence order: the --root flag,
// $FAB_ROOT, the nearest ancestor of the working directory containing
// foundry.yaml, and finally the working directory itself.
//
// The last fallback exists so that the commands that only read layers work in a
// bare directory, before `fab init` has produced a foundry.yaml.
func (f *ConfigFlags) FoundryRoot() (string, error) {
	if f.Root != nil && *f.Root != "" {
		return filepath.Abs(*f.Root)
	}
	if fromEnv := os.Getenv(FoundryRootEnvVar); fromEnv != "" {
		return filepath.Abs(fromEnv)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determining the working directory: %w", err)
	}
	if root, found := findFoundryRoot(workingDir); found {
		return root, nil
	}
	return workingDir, nil
}

// findFoundryRoot walks up from dir looking for a foundry.yaml.
func findFoundryRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, FoundryConfigFile)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func stringptr(value string) *string {
	return &value
}
