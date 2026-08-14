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

// Package gitutil reads just enough git state to record provenance on a
// published ontology.
package gitutil

import (
	"context"
	"os/exec"
	"strings"
)

// HeadRef returns the commit SHA checked out in dir, or the empty string when
// dir is not a git work tree, git is not installed, or the repository has no
// commits.
//
// Provenance is best-effort by design: publishing an ontology from a tarball or
// a CI scratch directory must not fail for want of a git repository.
func HeadRef(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
