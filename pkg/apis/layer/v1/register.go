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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupName is the API group of the layer manifest. It is the same group the
// ontology documents use: one apiVersion covers everything fab reads.
const GroupName = "fab"

// Version is the API version of the types in this package.
const Version = "v1"

// LayerKind is the kind of a layer.yaml document.
const LayerKind = "Layer"

// FileName is the name of a layer's manifest, at the root of the layer directory.
const FileName = "layer.yaml"

// SchemeGroupVersion is the group version these types are registered under. Its
// String form, "fab/v1", is what a manifest carries in apiVersion.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}
