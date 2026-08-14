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
	"github.com/GiorgosAlexakis/fab/pkg/util/naming"
)

// SetObjectDefaults applies the defaults for obj's kind. Defaulting runs after
// decoding and before validation, so validation only ever sees fully populated
// documents.
func SetObjectDefaults(obj Object) {
	switch typed := obj.(type) {
	case *ObjectType:
		SetDefaults_ObjectType(typed)
	case *LinkType:
		SetDefaults_LinkType(typed)
	}
}

// SetDefaults_ObjectType defaults an object type document.
//
//nolint:revive,stylecheck // Underscore naming matches the Kubernetes defaulter convention.
func SetDefaults_ObjectType(obj *ObjectType) {
	setTypeMetaDefaults(&obj.TypeMeta, ObjectTypeKind)

	for i := range obj.Spec.Properties {
		property := &obj.Spec.Properties[i]
		if property.Nullable == nil {
			// The primary key is the object's identity: it can never be absent.
			nullable := property.Name != obj.Spec.PrimaryKey
			property.Nullable = &nullable
		}
		if property.Name == obj.Spec.PrimaryKey {
			property.Unique = true
			property.Indexed = true
		}
		if property.Unique {
			// A uniqueness constraint is enforced by an index; saying "unique"
			// without "indexed" would be a lie about the storage layout.
			property.Indexed = true
		}
	}
}

// SetDefaults_LinkType defaults a link type document.
//
//nolint:revive,stylecheck // Underscore naming matches the Kubernetes defaulter convention.
func SetDefaults_LinkType(obj *LinkType) {
	setTypeMetaDefaults(&obj.TypeMeta, LinkTypeKind)

	if obj.Spec.Source.Layer == "" {
		obj.Spec.Source.Layer = obj.Metadata.Layer
	}
	if obj.Spec.Target.Layer == "" {
		obj.Spec.Target.Layer = obj.Metadata.Layer
	}
	if obj.Spec.ForwardName == "" {
		obj.Spec.ForwardName = naming.ToSnakeCase(obj.Metadata.Name)
	}
	if obj.Spec.ReverseName == "" {
		obj.Spec.ReverseName = naming.ToSnakeCase(obj.Spec.Source.Type)
	}
	if obj.Spec.OnSourceDelete == "" {
		obj.Spec.OnSourceDelete = DeletePolicyRestrict
	}
}

func setTypeMetaDefaults(meta *TypeMeta, kind string) {
	if meta.APIVersion == "" {
		meta.APIVersion = SchemeGroupVersion.String()
	}
	if meta.Kind == "" {
		meta.Kind = kind
	}
}
