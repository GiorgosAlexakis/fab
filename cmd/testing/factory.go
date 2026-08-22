package cmdtesting

import (
	"errors"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

type TestFactory struct {
	Root string
}

var _ cmdutil.Factory = &TestFactory{}

func NewTestFactory(root string) *TestFactory {
	return &TestFactory{Root: root}
}

func (f *TestFactory) FoundryRoot() (string, error) {
	if f.Root == "" {
		return "", errors.New("no foundry root configured in the test factory")
	}
	return f.Root, nil
}

func (f *TestFactory) Layers() ([]*layerv1.Layer, error) {
	root, err := f.FoundryRoot()
	if err != nil {
		return nil, err
	}
	return cmdutil.Discover(root)
}
