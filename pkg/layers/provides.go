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

package layers

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Contributions is what a layer actually ships, discovered by compiling its
// schema directory.
type Contributions struct {
	// Objects are the object type names.
	Objects []string
	// Links are the link type names.
	Links []string
}

// CheckProvides compares each layer's declared contributions against what its
// schema directory actually holds.
//
// The declaration is how one layer team tells another what to expect, so a
// manifest that disagrees with the tree is a bug in the layer. Only layers that
// declare provides.schema are checked: leaving it out means "read the tree", not
// "I ship nothing".
//
// Kinds fab does not compile yet -- aspects, interfaces, actions -- are ignored
// rather than reported as undeclared, because a layer may legitimately ship them
// ahead of the compiler support.
func CheckProvides(resolution *Resolution, actual map[string]Contributions) error {
	var problems []error

	for _, layer := range resolution.Ordered {
		declared := layer.Manifest.Spec.Provides.Schema
		if declared == nil {
			continue
		}

		shipped := actual[layer.Name()]
		problems = append(problems,
			compare(layer, "object types", declared.Objects, shipped.Objects)...)
		problems = append(problems,
			compare(layer, "link types", declared.Links, shipped.Links)...)
	}

	return errors.NewAggregate(problems)
}

func compare(layer Layer, kind string, declared, shipped []string) []error {
	var problems []error

	declaredSet := sets.New(declared...)
	shippedSet := sets.New(shipped...)

	if missing := declaredSet.Difference(shippedSet); missing.Len() > 0 {
		problems = append(problems, fmt.Errorf("%s declares %s it does not ship: %s",
			layer.Path, kind, describe(sortedList(missing))))
	}
	if extra := shippedSet.Difference(declaredSet); extra.Len() > 0 {
		problems = append(problems, fmt.Errorf("%s ships %s it does not declare: %s",
			layer.Path, kind, describe(sortedList(extra))))
	}

	return problems
}

func sortedList(values sets.Set[string]) []string {
	result := values.UnsortedList()
	sort.Strings(result)
	return result
}
