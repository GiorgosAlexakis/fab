package validation

import (
	"strings"
	"testing"
)

func TestNameProblems(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		wantOK  bool
		wantSay string
	}{
		{name: "a layer name", value: "meta-auth", wantOK: true},
		{name: "a foundry name", value: "acme-corp", wantOK: true},
		{name: "digits", value: "meta-s3", wantOK: true},
		{name: "a single character", value: "a", wantOK: true},
		{name: "empty", value: "", wantSay: "required"},
		{name: "capitals", value: "Meta-Auth", wantSay: "lowercase"},
		{name: "a space", value: "acme corp", wantSay: "lowercase"},
		{name: "an underscore", value: "meta_auth", wantSay: "lowercase"},
		{name: "a dot", value: "acme.corp", wantSay: "lowercase"},
		{name: "a leading hyphen", value: "-meta", wantSay: "start and end"},
		{name: "a trailing hyphen", value: "meta-", wantSay: "start and end"},
		{name: "a path separator", value: "layers/meta-auth", wantSay: "lowercase"},
		{name: "too long", value: strings.Repeat("a", NameMaxLength+1), wantSay: "at most"},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems := NameProblems(test.value)

			if test.wantOK {
				if len(problems) > 0 {
					t.Fatalf("NameProblems(%q) = %v, want none", test.value, problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("NameProblems(%q) found nothing wrong", test.value)
			}
			if !strings.Contains(strings.Join(problems, " "), test.wantSay) {
				t.Errorf("NameProblems(%q) = %v, want one mentioning %q", test.value, problems, test.wantSay)
			}
		})
	}
}
