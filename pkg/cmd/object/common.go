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

package object

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	objectstorepostgres "github.com/GiorgosAlexakis/fab/pkg/objectstore/postgres"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

// storeAccess is embedded by every object command. They all need the same thing:
// a store bound to one ontology version, because a write can only be validated
// against a specific version of the schema.
type storeAccess struct {
	// Version pins the ontology version to bind to. Empty means "resolve the
	// environment tag".
	Version string

	// Store is the bound object store.
	Store objectstore.Interface
	// Binding is the ontology the store is bound to. Commands use it to resolve
	// property names before they build a request, so that a typo fails before
	// anything is written.
	Binding *objectstore.Binding
}

// addFlags binds the ontology selector. The tag lives on the root command, since
// it is usually set once per environment; pinning a version is per-invocation.
func (a *storeAccess) addFlags(flags *pflag.FlagSet) {
	flags.StringVar(&a.Version, "ontology-version", a.Version,
		"Ontology version to bind to, e.g. 1.3.0. Defaults to the version --ontology-tag resolves to.")
}

// complete binds the store to an ontology version and connects to the object
// store.
func (a *storeAccess) complete(ctx context.Context, f cmdutil.Factory) error {
	name, err := f.OntologyName()
	if err != nil {
		return err
	}
	client, err := f.Registry(ctx)
	if err != nil {
		return err
	}

	if a.Version != "" {
		a.Binding, err = objectstore.BindVersion(ctx, client, name, a.Version)
	} else {
		var tag string
		if tag, err = f.OntologyTag(); err == nil {
			a.Binding, err = objectstore.BindTag(ctx, client, name, tag)
		}
	}
	if err != nil {
		return err
	}

	db, err := f.ObjectStoreDB(ctx)
	if err != nil {
		return err
	}
	a.Store = objectstorepostgres.New(db, a.Binding)
	return nil
}

// objectType resolves a type name against the bound ontology, reporting what is
// available when it does not resolve: a caller who misremembers a type name is
// better served by the list than by the failure.
func (a *storeAccess) objectType(name string) (*objectstore.ObjectType, error) {
	objectType, err := a.Binding.ObjectType(name)
	if err != nil {
		return nil, fmt.Errorf("%w\nknown object types: %s",
			err, strings.Join(a.Binding.ObjectTypeNames(), ", "))
	}
	return objectType, nil
}

// linkType resolves a link type name against the bound ontology.
func (a *storeAccess) linkType(name string) (objectstore.LinkType, error) {
	linkType, err := a.Binding.LinkType(name)
	if err != nil {
		return objectstore.LinkType{}, fmt.Errorf("%w\nknown link types: %s",
			err, strings.Join(a.Binding.LinkTypeNames(), ", "))
	}
	return linkType, nil
}

// parseAssignments turns `name=value` flags into typed property values, using
// the ontology to decide what each text means.
func parseAssignments(
	objectType *objectstore.ObjectType,
	assignments []string,
) (map[string]interface{}, error) {
	values := make(map[string]interface{}, len(assignments))

	for _, assignment := range assignments {
		name, text, found := strings.Cut(assignment, "=")
		if !found {
			return nil, fmt.Errorf("%q is not of the form property=value", assignment)
		}
		property, ok := objectType.Property(name)
		if !ok {
			return nil, fmt.Errorf("%s has no property %q: %w\nknown properties: %s",
				objectType.QualifiedName, name, objectstore.ErrUnknownProperty,
				strings.Join(propertyNames(objectType), ", "))
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("property %q is set twice", name)
		}

		value, err := objectstore.ParseValue(property, text)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}

	return values, nil
}

// propertyNames returns the properties of a type, sorted.
func propertyNames(objectType *objectstore.ObjectType) []string {
	names := make([]string, 0, len(objectType.Properties))
	for name := range objectType.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// printObject writes one object in the requested format.
func printObject(out io.Writer, format string, object *objectstore.Object) error {
	switch format {
	case printers.FormatJSON:
		return printers.JSON(out, object)
	case printers.FormatYAML:
		return printers.YAML(out, object)
	default:
		return printers.Object(out, object)
	}
}

// printObjects writes a list of objects in the requested format, with one column
// per property of the type.
func printObjects(
	out io.Writer,
	format string,
	objectType *objectstore.ObjectType,
	objects []objectstore.Object,
) error {
	switch format {
	case printers.FormatJSON:
		return printers.JSON(out, objects)
	case printers.FormatYAML:
		return printers.YAML(out, objects)
	}

	if len(objects) == 0 {
		_, err := fmt.Fprintf(out, "No %s objects found.\n", objectType.QualifiedName)
		return err
	}
	return printers.ObjectList(out, objects, listedProperties(objectType))
}

// listedProperties are the columns of a table of objects: every property except
// the primary key, which the table already leads with.
func listedProperties(objectType *objectstore.ObjectType) []string {
	names := propertyNames(objectType)
	columns := make([]string, 0, len(names))
	for _, name := range names {
		if name == objectType.PrimaryKey.Name {
			continue
		}
		columns = append(columns, name)
	}
	return columns
}

// validateObjectOutputFormat accepts the formats that make sense for object
// instances. A digest is a property of an ontology, not of an object.
func validateObjectOutputFormat(format string) error {
	switch format {
	case printers.FormatTable, printers.FormatJSON, printers.FormatYAML:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be one of json|yaml", format)
	}
}
