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

// Package compiler merges the schema documents of every active layer into a
// single snapshot.
//
// Cross-layer references are resolved here, at compile time. A link that points
// at a type from an inactive layer is a compile error, never a runtime failure.
package compiler

import (
	"fmt"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/apis/ontology/validation"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
)

// Document is one decoded ontology document together with where it came from,
// so that every error can name a file.
type Document struct {
	// Source is a human-readable origin, typically a file path.
	Source string
	// Object is the decoded, defaulted document.
	Object ontologyv1.Object
}

// LayerSource is everything one layer contributes to the ontology.
type LayerSource struct {
	// Layer is the layer name, e.g. meta-core or app.
	Layer string
	// Documents are the layer's schema documents.
	Documents []Document
}

// Compile merges the given layers into a snapshot. Layers must be supplied in
// merge order, which is the topological order of the layer dependency graph;
// the resolver owns that ordering, the compiler trusts it.
//
// All errors are collected before returning so that a malformed schema tree
// reports every problem in one run rather than one problem per run.
func Compile(layers []LayerSource) (*snapshot.Snapshot, error) {
	result := &snapshot.Snapshot{Layers: make([]string, 0, len(layers))}
	var errs []error

	seenLayers := sets.New[string]()
	objectTypeSources := map[string]string{}
	linkTypeSources := map[string]string{}

	for _, layer := range layers {
		if layer.Layer == "" {
			errs = append(errs, fmt.Errorf("layer name must not be empty"))
			continue
		}
		if seenLayers.Has(layer.Layer) {
			errs = append(errs, fmt.Errorf("layer %q is declared more than once", layer.Layer))
			continue
		}
		seenLayers.Insert(layer.Layer)
		result.Layers = append(result.Layers, layer.Layer)

		for _, document := range layer.Documents {
			errs = append(errs, compileDocument(result, layer.Layer, document, objectTypeSources, linkTypeSources)...)
		}
	}

	errs = append(errs, resolveLinkTypes(result, linkTypeSources)...)

	if err := utilerrors.NewAggregate(errs); err != nil {
		return nil, err
	}

	result.Normalize()
	return result, nil
}

func compileDocument(
	result *snapshot.Snapshot,
	layer string,
	document Document,
	objectTypeSources map[string]string,
	linkTypeSources map[string]string,
) []error {
	object := document.Object
	metadata := object.GetObjectMeta()

	// The owning layer is the layer that ships the file. A document may state
	// it explicitly, but it may not claim to belong to a different layer: that
	// would let a layer define types inside someone else's namespace.
	switch metadata.Layer {
	case "":
		metadata.Layer = layer
	case layer:
	default:
		return []error{fmt.Errorf("%s: metadata.layer is %q but the document is contributed by layer %q",
			document.Source, metadata.Layer, layer)}
	}

	// Defaults that depend on metadata.layer, such as an omitted link endpoint
	// layer, can only be applied once the owning layer is known.
	ontologyv1.SetObjectDefaults(object)

	if fieldErrs := validation.ValidateObject(object); len(fieldErrs) > 0 {
		return prefixFieldErrors(document.Source, fieldErrs)
	}

	switch typed := object.(type) {
	case *ontologyv1.ObjectType:
		qualifiedName := snapshot.QualifiedName(typed.Metadata.Layer, typed.Metadata.Name)
		if previous, ok := objectTypeSources[qualifiedName]; ok {
			return []error{fmt.Errorf("%s: object type %s is already defined in %s: a type has exactly one owning layer",
				document.Source, qualifiedName, previous)}
		}
		objectTypeSources[qualifiedName] = document.Source
		result.ObjectTypes = append(result.ObjectTypes, convertObjectType(typed))
	case *ontologyv1.LinkType:
		qualifiedName := snapshot.QualifiedName(typed.Metadata.Layer, typed.Metadata.Name)
		if previous, ok := linkTypeSources[qualifiedName]; ok {
			return []error{fmt.Errorf("%s: link type %s is already defined in %s",
				document.Source, qualifiedName, previous)}
		}
		linkTypeSources[qualifiedName] = document.Source
		result.LinkTypes = append(result.LinkTypes, convertLinkType(typed))
	default:
		return []error{fmt.Errorf("%s: no compiler support for %T", document.Source, object)}
	}

	return nil
}

// resolveLinkTypes checks every link endpoint against the merged object types
// and rejects traversal names that would be ambiguous on either endpoint.
func resolveLinkTypes(result *snapshot.Snapshot, linkTypeSources map[string]string) []error {
	var errs []error

	// Traversal names must be unique per object type, and must not collide with
	// that type's property names: `customer.orders` and `customer.email` are
	// resolved in the same namespace by the OSDK.
	traversals := map[string]string{}

	claimTraversal := func(source, owner, traversalName, describe string) {
		key := owner + "." + traversalName
		if previous, ok := traversals[key]; ok {
			errs = append(errs, fmt.Errorf("%s: %s %q on %s collides with %s",
				source, describe, traversalName, owner, previous))
			return
		}
		traversals[key] = describe
	}

	for i := range result.LinkTypes {
		link := &result.LinkTypes[i]
		source := linkTypeSources[link.QualifiedName()]

		sourceType, sourceFound := result.ObjectType(link.Source.Layer, link.Source.Type)
		if !sourceFound {
			errs = append(errs, fmt.Errorf("%s: link type %s references unknown source object type %s: "+
				"either the type name is wrong or layer %q is not active",
				source, link.QualifiedName(), link.Source.QualifiedName(), link.Source.Layer))
		}
		targetType, targetFound := result.ObjectType(link.Target.Layer, link.Target.Type)
		if !targetFound {
			errs = append(errs, fmt.Errorf("%s: link type %s references unknown target object type %s: "+
				"either the type name is wrong or layer %q is not active",
				source, link.QualifiedName(), link.Target.QualifiedName(), link.Target.Layer))
		}
		if !sourceFound || !targetFound {
			continue
		}

		if _, exists := sourceType.Property(link.ForwardName); exists {
			errs = append(errs, fmt.Errorf("%s: forwardName %q on %s collides with a property of that type",
				source, link.ForwardName, sourceType.QualifiedName()))
		}
		if _, exists := targetType.Property(link.ReverseName); exists {
			errs = append(errs, fmt.Errorf("%s: reverseName %q on %s collides with a property of that type",
				source, link.ReverseName, targetType.QualifiedName()))
		}

		claimTraversal(source, sourceType.QualifiedName(), link.ForwardName,
			fmt.Sprintf("forward traversal of link %s", link.QualifiedName()))
		claimTraversal(source, targetType.QualifiedName(), link.ReverseName,
			fmt.Sprintf("reverse traversal of link %s", link.QualifiedName()))
	}

	return errs
}

func convertObjectType(in *ontologyv1.ObjectType) snapshot.ObjectType {
	out := snapshot.ObjectType{
		Layer:       in.Metadata.Layer,
		Name:        in.Metadata.Name,
		Description: in.Metadata.Description,
		PrimaryKey:  in.Spec.PrimaryKey,
		Properties:  make([]snapshot.Property, 0, len(in.Spec.Properties)),
	}
	for i := range in.Spec.Properties {
		property := &in.Spec.Properties[i]
		out.Properties = append(out.Properties, snapshot.Property{
			Name:        property.Name,
			Type:        string(property.Type),
			Description: property.Description,
			Items:       string(property.Items),
			Values:      property.Values,
			Nullable:    property.IsNullable(),
			Unique:      property.Unique,
			Indexed:     property.Indexed,
		})
	}
	return out
}

func convertLinkType(in *ontologyv1.LinkType) snapshot.LinkType {
	return snapshot.LinkType{
		Layer:          in.Metadata.Layer,
		Name:           in.Metadata.Name,
		Description:    in.Metadata.Description,
		Source:         snapshot.TypeRef{Layer: in.Spec.Source.Layer, Type: in.Spec.Source.Type},
		Target:         snapshot.TypeRef{Layer: in.Spec.Target.Layer, Type: in.Spec.Target.Type},
		Cardinality:    string(in.Spec.Cardinality),
		ForwardName:    in.Spec.ForwardName,
		ReverseName:    in.Spec.ReverseName,
		OnSourceDelete: string(in.Spec.OnSourceDelete),
	}
}

func prefixFieldErrors(source string, fieldErrs field.ErrorList) []error {
	errs := make([]error, 0, len(fieldErrs))
	for _, fieldErr := range fieldErrs {
		errs = append(errs, fmt.Errorf("%s: %s", source, fieldErr.Error()))
	}
	return errs
}
