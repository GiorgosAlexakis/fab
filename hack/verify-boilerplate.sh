#!/usr/bin/env bash
# Copyright The FAB Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

expected_copyright="Copyright The FAB Authors."

failed=()
while IFS= read -r file; do
  # gofmt hoists //go:build constraints above the license block, so look at the
  # head of the file rather than at a fixed line.
  if ! head -n 6 "${file}" | grep -qxF "${expected_copyright}"; then
    failed+=("${file}")
  fi
done < <(find . -name '*.go' -not -path './vendor/*' -not -path './.git/*' | sort)

if [[ ${#failed[@]} -gt 0 ]]; then
  echo "The following files are missing the boilerplate header from hack/boilerplate/boilerplate.go.txt:" >&2
  for file in "${failed[@]}"; do
    echo "  ${file}" >&2
  done
  exit 1
fi

echo "All Go files carry the expected boilerplate header."
