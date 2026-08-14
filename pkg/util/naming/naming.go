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

// Package naming converts between the naming conventions the ontology uses:
// PascalCase for type names, snake_case for properties and traversal names.
package naming

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts a PascalCase or camelCase identifier to snake_case.
// Runs of capitals are treated as a single word, so "IMONumber" becomes
// "imo_number" rather than "i_m_o_number".
func ToSnakeCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)

	runes := []rune(s)
	for i, r := range runes {
		if r == '_' || r == '-' || r == ' ' {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			// Start a new word at a lower-to-upper transition ("aB"), or at the
			// last capital of a run followed by a lowercase ("HTTPServer").
			startsWord := !unicode.IsUpper(prev) && prev != '_'
			endsAcronym := unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if (startsWord || endsAcronym) && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
