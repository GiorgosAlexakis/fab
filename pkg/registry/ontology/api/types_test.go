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

package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// TestErrorsRoundTrip checks that a registry error survives the trip through
// HTTP, because callers branch on these with errors.Is: a publish that hits
// ErrAlreadyExists is a pipeline re-run, while a generic failure is an outage.
func TestErrorsRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantIs   error
	}{
		{
			name:     "not found",
			err:      fmt.Errorf("acme-corp:1.0.0: %w", registry.ErrNotFound),
			wantCode: http.StatusNotFound,
			wantIs:   registry.ErrNotFound,
		},
		{
			name:     "already exists",
			err:      fmt.Errorf("acme-corp:1.0.0 is already published: %w", registry.ErrAlreadyExists),
			wantCode: http.StatusConflict,
			wantIs:   registry.ErrAlreadyExists,
		},
		{
			name:     "not published",
			err:      fmt.Errorf("1.1.0 is a draft: %w", registry.ErrNotPublished),
			wantCode: http.StatusConflict,
			wantIs:   registry.ErrNotPublished,
		},
		{
			name:     "no previous version",
			err:      fmt.Errorf("prod: %w", registry.ErrNoPreviousVersion),
			wantCode: http.StatusConflict,
			wantIs:   registry.ErrNoPreviousVersion,
		},
		{
			name:     "digest mismatch",
			err:      fmt.Errorf("acme-corp:1.0.0: %w", registry.ErrDigestMismatch),
			wantCode: http.StatusInternalServerError,
			wantIs:   registry.ErrDigestMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, reason := StatusForError(test.err)
			if code != test.wantCode {
				t.Errorf("code = %d, want %d", code, test.wantCode)
			}

			decoded := ErrorFromStatus(apiserver.Status{Reason: reason, Message: test.err.Error()})
			if !errors.Is(decoded, test.wantIs) {
				t.Errorf("decoded error %v does not match %v", decoded, test.wantIs)
			}
			// The message must not accumulate the sentinel on every hop.
			if got, want := decoded.Error(), test.err.Error(); got != want {
				t.Errorf("decoded message = %q, want %q", got, want)
			}
		})
	}
}

// TestUnknownReasonKeepsTheMessage covers a failure a client cannot classify: a
// bad request, or a proxy answering instead of the server.
func TestUnknownReasonKeepsTheMessage(t *testing.T) {
	err := ErrorFromStatus(apiserver.Status{Reason: ReasonInvalid, Message: "version is required"})
	if err.Error() != "version is required" {
		t.Errorf("error = %q, want the server message", err.Error())
	}

	status, ok := apiserver.StatusFromError(err)
	if !ok {
		t.Fatal("an unclassified error should still carry its Status")
	}
	if status.Reason != ReasonInvalid {
		t.Errorf("reason = %q, want %q", status.Reason, ReasonInvalid)
	}
}

func TestStatusForUnknownErrorIsInternal(t *testing.T) {
	code, reason := StatusForError(errors.New("connection reset"))
	if code != http.StatusInternalServerError || reason != ReasonInternal {
		t.Errorf("StatusForError() = %d, %q, want 500, %q", code, reason, ReasonInternal)
	}
}
