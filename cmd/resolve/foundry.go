package resolve

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
)

type ResolvedFoundry struct {
	Root       string
	Manifest   *foundryv1.Foundry
	Resolution *Resolution
}

func ResolveFoundry(root string) (*ResolvedFoundry, error) {
	manifest, err := foundryv1.NewEngine(root).Load()
	if err != nil {
		if errors.Is(err, foundryv1.ErrFoundryNotFound) {
			return nil, fmt.Errorf("%w: run `fab init` to create one, or pass --root", err)
		}
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w",
			filepath.Join(root, cmdutil.FoundryFileName), err)
	}

	discovered, err := cmdutil.Discover(root)
	if err != nil {
		return nil, err
	}

	resolution, err := Resolve(manifest, discovered)
	if err != nil {
		return nil, err
	}
	return &ResolvedFoundry{Root: root, Manifest: manifest, Resolution: resolution}, nil
}

func WriteLock(resolved *ResolvedFoundry, bundle *foundryv1.Bundle) (*foundryv1.Lock, error) {
	lock, err := NewLock(resolved.Root, resolved.Resolution)
	if err != nil {
		return nil, err
	}

	existing, err := cmdutil.LoadLock(resolved.Root)
	switch {
	case err == nil:
		if bundle == nil {
			bundle = existing.Bundle
		}
	case errors.Is(err, cmdutil.ErrNoLock):
	default:
		return nil, err
	}
	lock.Bundle = bundle
	if err := cmdutil.SaveLock(resolved.Root, lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func CheckLock(resolved *ResolvedFoundry) error {
	existing, err := cmdutil.LoadLock(resolved.Root)
	if err != nil {
		if errors.Is(err, cmdutil.ErrNoLock) {
			return fmt.Errorf("%s does not exist: run `fab resolve` and commit it", cmdutil.LockFileName)
		}
		return err
	}

	changes, err := DiffLock(existing, resolved.Root, resolved.Resolution)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("%s is out of date; run `fab resolve`:\n  %s",
			cmdutil.LockFileName, strings.Join(changes, "\n  "))
	}
	return nil
}
