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

// Package v1 contains the fab/v1 Layer API type: the layer.yaml manifest at the
// root of every layer.
//
// A layer manifest is not an ontology document, which is why it lives in its own
// package rather than alongside ObjectType and LinkType even though both carry
// apiVersion fab/v1. The two sets of kinds are read by different loaders from
// different places, and each must reject the other's kinds: a Layer found under
// schema/ is a mistake, and so is an ObjectType found as layer.yaml.
package v1 // import "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
