package util

import (
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

type Factory interface {
	FoundryRoot() (string, error)
	Layers() ([]*layerv1.Layer, error)
}

type FoundryLocator interface {
	FoundryRoot() (string, error)
}

type factoryImpl struct {
	locator FoundryLocator
}

func NewFactory(locator FoundryLocator) Factory {
	return &factoryImpl{locator: locator}
}

func (f *factoryImpl) FoundryRoot() (string, error) {
	return f.locator.FoundryRoot()
}

func (f *factoryImpl) Layers() ([]*layerv1.Layer, error) {
	root, err := f.FoundryRoot()
	if err != nil {
		return nil, err
	}
	return Discover(root)
}
