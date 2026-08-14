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

package util

import (
	"errors"
	"strings"
	"testing"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

func TestCheckErr(t *testing.T) {
	testCases := []struct {
		name         string
		err          error
		wantHandled  bool
		wantContains []string
	}{
		{
			name:        "no error",
			err:         nil,
			wantHandled: false,
		},
		{
			name:         "single error",
			err:          errors.New("schema/objects/customer.yaml: spec.primaryKey: Required value"),
			wantHandled:  true,
			wantContains: []string{"spec.primaryKey"},
		},
		{
			name:         "aggregate of one is reported as a plain error",
			err:          utilerrors.NewAggregate([]error{errors.New("only one problem")}),
			wantHandled:  true,
			wantContains: []string{"only one problem"},
		},
		{
			name: "aggregate of several is listed",
			err: utilerrors.NewAggregate([]error{
				errors.New("first problem"),
				errors.New("second problem"),
			}),
			wantHandled:  true,
			wantContains: []string{"2 problems found", "first problem", "second problem"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var message string
			var code int
			handled := false

			checkErr(testCase.err, func(gotMessage string, gotCode int) {
				handled = true
				message = gotMessage
				code = gotCode
			})

			if handled != testCase.wantHandled {
				t.Fatalf("handled = %v, want %v", handled, testCase.wantHandled)
			}
			if !handled {
				return
			}
			if code != DefaultErrorExitCode {
				t.Errorf("exit code = %d, want %d", code, DefaultErrorExitCode)
			}
			for _, want := range testCase.wantContains {
				if !strings.Contains(message, want) {
					t.Errorf("message %q is missing %q", message, want)
				}
			}
			if strings.Contains(message, "1 problems") {
				t.Errorf("message uses a plural count for a single problem: %q", message)
			}
		})
	}
}
