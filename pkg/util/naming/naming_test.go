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

package naming

import "testing"

func TestToSnakeCase(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single word", input: "Customer", want: "customer"},
		{name: "two words", input: "PurchaseOrder", want: "purchase_order"},
		{name: "three words", input: "VesselVoyageLeg", want: "vessel_voyage_leg"},
		{name: "leading acronym", input: "IMONumber", want: "imo_number"},
		{name: "trailing acronym", input: "CustomerID", want: "customer_id"},
		{name: "already snake case", input: "last_login", want: "last_login"},
		{name: "camel case", input: "lastLogin", want: "last_login"},
		{name: "digits stay attached", input: "Address2", want: "address2"},
		{name: "dashes become underscores", input: "meta-auth", want: "meta_auth"},
		{name: "empty", input: "", want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ToSnakeCase(testCase.input); got != testCase.want {
				t.Errorf("ToSnakeCase(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}
