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

package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	var body struct {
		Version string `json:"version"`
	}

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"versionn":"1.0.0"}`))
	if err := DecodeJSON(request, &body); err == nil {
		t.Fatal("DecodeJSON accepted a field the server does not understand")
	}

	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"version":"1.0.0"}`))
	if err := DecodeJSON(request, &body); err != nil {
		t.Fatalf("DecodeJSON rejected a valid body: %v", err)
	}
	if body.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", body.Version)
	}
}

func TestQueryInt(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?limit=25&bad=x&negative=-1", nil)

	if got, err := QueryInt(request, "limit", 100); err != nil || got != 25 {
		t.Errorf("QueryInt(limit) = %d, %v, want 25, nil", got, err)
	}
	if got, err := QueryInt(request, "missing", 100); err != nil || got != 100 {
		t.Errorf("QueryInt(missing) = %d, %v, want the fallback", got, err)
	}
	if _, err := QueryInt(request, "bad", 0); err == nil {
		t.Error("QueryInt accepted a non-integer")
	}
	if _, err := QueryInt(request, "negative", 0); err == nil {
		t.Error("QueryInt accepted a negative value")
	}
}

func TestQueryBool(t *testing.T) {
	// A bare parameter reads as true, so "?total" means what a reader expects.
	request := httptest.NewRequest(http.MethodGet, "/?total&explicit=false&bad=maybe", nil)

	if got, err := QueryBool(request, "total"); err != nil || !got {
		t.Errorf("QueryBool(total) = %v, %v, want true, nil", got, err)
	}
	if got, err := QueryBool(request, "explicit"); err != nil || got {
		t.Errorf("QueryBool(explicit) = %v, %v, want false, nil", got, err)
	}
	if got, err := QueryBool(request, "missing"); err != nil || got {
		t.Errorf("QueryBool(missing) = %v, %v, want false, nil", got, err)
	}
	if _, err := QueryBool(request, "bad"); err == nil {
		t.Error("QueryBool accepted a non-boolean")
	}
}

func TestWriteErrorCarriesAReason(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteError(recorder, http.StatusNotFound, "NotFound", Status{Message: "acme-corp:1.0.0"})

	if recorder.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"reason":"NotFound"`) {
		t.Errorf("body = %s, want it to carry the reason", body)
	}
}
