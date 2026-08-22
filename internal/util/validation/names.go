// Package validation holds the checks the API packages share.
package validation

import (
	"fmt"
	"regexp"
)

// NameMaxLength bounds a fab name. Nothing needs a name this long: the bound is
// there so that a name stays readable in the table `fab layers` prints, and it
// sits far under what any filesystem accepts for a directory.
const NameMaxLength = 63

// nameFmt is a lowercase word of letters, digits and hyphens, starting and
// ending on a letter or a digit.
const nameFmt = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"

var nameRegexp = regexp.MustCompile("^" + nameFmt + "$")

// NameProblems returns what is wrong with name, and nothing when it is fine.
//
// A fab name is also a directory name. A layer lives in layers/<name>, and
// `fab init acme-corp` creates ./acme-corp. Holding names to lowercase letters,
// digits and hyphens keeps the manifest and the directory the same string on
// every machine, whatever the filesystem does with case, spaces and separators.
func NameProblems(name string) []string {
	var problems []string

	// An empty name is reported as missing rather than as malformed: telling a
	// user which characters are allowed says nothing when they wrote none.
	if name == "" {
		return []string{"is required"}
	}
	if len(name) > NameMaxLength {
		problems = append(problems, fmt.Sprintf("must be at most %d characters", NameMaxLength))
	}
	if !nameRegexp.MatchString(name) {
		problems = append(problems,
			"must be lowercase letters, digits or '-', and must start and end with a letter or a digit (e.g. meta-auth)")
	}
	return problems
}
