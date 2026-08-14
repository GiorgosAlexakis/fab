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

package v1

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupName is the API group of the ontology types. It is the "fab" in
// `apiVersion: fab/v1`.
const GroupName = "fab"

// Version is the API version of the types in this package.
const Version = "v1"

// Kinds understood by Phase 1 of the ontology.
const (
	// ObjectTypeKind is the kind of an ObjectType document.
	ObjectTypeKind = "ObjectType"
	// LinkTypeKind is the kind of a LinkType document.
	LinkTypeKind = "LinkType"
)

// SchemeGroupVersion is the group version used to register these types. Its
// String form, "fab/v1", is what documents carry in apiVersion.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

// constructors maps a kind to a factory for an empty object of that kind. A
// kind that is not registered here is rejected by the loader, which is how
// Phase 2 and Phase 3 kinds produce a clear "not supported yet" error instead
// of being silently ignored.
var constructors = map[string]func() Object{
	ObjectTypeKind: func() Object {
		return &ObjectType{TypeMeta: TypeMeta{APIVersion: SchemeGroupVersion.String(), Kind: ObjectTypeKind}}
	},
	LinkTypeKind: func() Object {
		return &LinkType{TypeMeta: TypeMeta{APIVersion: SchemeGroupVersion.String(), Kind: LinkTypeKind}}
	},
}

// New returns an empty object of the given kind with its TypeMeta populated.
func New(kind string) (Object, error) {
	constructor, ok := constructors[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported kind %q: known kinds are %v", kind, KnownKinds())
	}
	return constructor(), nil
}

// KnownKinds returns the registered kinds in stable order.
func KnownKinds() []string {
	kinds := make([]string, 0, len(constructors))
	for kind := range constructors {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
