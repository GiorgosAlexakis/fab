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

// Package v1 contains the fab/v1 ontology API types: the YAML documents an FDE
// writes under schema/ and a layer ships under layers/<layer>/schema/.
//
// Phase 1 of the ELO ontology covers ObjectType, Property and LinkType.
// Interface and Aspect (Phase 2), ActionType (Phase 3) and AccessPolicy
// (Phase 5) are deliberately absent; adding a kind means adding it to the
// scheme in register.go together with its validation.
//
// These types are the source of truth. Everything downstream -- the compiled
// snapshot, the registry rows, generated proto, generated SQL -- is derived.
package v1 // import "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
