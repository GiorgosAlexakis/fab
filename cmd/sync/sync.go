package sync

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/cmd/resolve"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
	"github.com/GiorgosAlexakis/fab/internal/util/printers"
)

const syncLong = `Make the layers foundry.yaml activates available under layers/.

The upstream layers are one bundle at one commit rather than a repository each, so a
single pin in foundry.lock fixes all of them as a compatible set. The bundle is
cloned into .fab/cache, which is gitignored, and each activated upstream layer is
linked into layers/ from there. Company-owned layers are real directories and are
never touched.

A foundry that is already synced to its pin does no work and needs no network. After
the layers are in place, fab sync runs the same check as fab resolve --check so a
sync leaves a foundry whose lock matches what is on disk.`

const syncExample = `  # Fetch and link the activated layers
  fab sync

  # Move the foundry to a different bundle release
  fab sync --bundle-ref v1.2.0`

type Options struct {
	genericiooptions.IOStreams

	BundleURL string
	BundleRef string
	Output    string

	root string
}

func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

func NewCmdSync(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:     "sync",
		Short:   "Fetch and link the layers this foundry activates",
		Long:    syncLong,
		Example: syncExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.BundleURL, "bundle-url", o.BundleURL,
		fmt.Sprintf("Repository the upstream layer bundle is fetched from. Defaults to the pin in %s, else %s.",
			cmdutil.LockFileName, DefaultBundleURL))
	cmd.Flags().StringVar(&o.BundleRef, "bundle-ref", o.BundleRef,
		fmt.Sprintf("Ref of the upstream layer bundle to fetch. Defaults to the pin in %s, else %s.",
			cmdutil.LockFileName, DefaultBundleRef))
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		fmt.Sprintf("Output format. One of: %v. Defaults to a summary table.", printers.SupportedFormats()))

	return cmd
}

func (o *Options) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	root, err := f.FoundryRoot()
	if err != nil {
		return err
	}
	o.root = root
	return nil
}

func (o *Options) Validate() error {
	return printers.ValidateFormat(o.Output)
}

func (o *Options) Run() error {
	result, err := Sync(SyncOptions{
		Root:      o.root,
		BundleURL: o.BundleURL,
		BundleRef: o.BundleRef,
	})
	if err != nil {
		return err
	}

	resolved, err := resolve.ResolveFoundry(o.root)
	if err != nil {
		return err
	}
	lock, err := resolve.WriteLock(resolved, result.Bundle)
	if err != nil {
		return err
	}
	if err := resolve.CheckLock(resolved); err != nil {
		return err
	}

	return o.print(result, lock)
}

func (o *Options) print(result *SyncResult, lock *foundryv1.Lock) error {
	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, lock)
	case printers.FormatYAML:
		return printers.YAML(o.Out, lock)
	}

	if lock.Bundle != nil {
		if err := printers.BundleSummary(o.Out, lock.Bundle); err != nil {
			return err
		}
		fmt.Fprintln(o.Out)
	}
	if len(lock.Locked) > 0 {
		if err := printers.LockSummary(o.Out, lock); err != nil {
			return err
		}
	}

	switch {
	case result.Fetched:
		fmt.Fprintf(o.ErrOut, "Linked %d upstream layers and wrote %s.\n",
			len(result.Linked), cmdutil.LockFileName)
	default:
		fmt.Fprintf(o.ErrOut, "Already synced; wrote %s.\n", cmdutil.LockFileName)
	}
	return nil
}
