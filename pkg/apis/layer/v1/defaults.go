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

// SetDefaults_Layer defaults a layer manifest.
//
//nolint:revive,stylecheck // Underscore naming matches the Kubernetes defaulter convention.
func SetDefaults_Layer(obj *Layer) {
	if obj.APIVersion == "" {
		obj.APIVersion = SchemeGroupVersion.String()
	}
	if obj.Kind == "" {
		obj.Kind = LayerKind
	}
	if obj.Metadata.Origin == "" {
		// A layer fab did not fetch is one you wrote, so the safe default is the
		// one fab will never upgrade behind your back.
		obj.Metadata.Origin = OriginLocal
	}
}
