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
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// EnvOr resolves a setting from a flag, then the environment, then a fallback.
//
// Both servers are configured by flags for a developer and by environment
// variables for a container, and neither should win by accident: an explicit
// flag beats the environment, and the environment beats the default.
func EnvOr(flagValue, envVar, fallback string) string {
	if flagValue != "" {
		return flagValue
	}
	if fromEnv := os.Getenv(envVar); fromEnv != "" {
		return fromEnv
	}
	return fallback
}

// NewLogger returns a structured logger writing to out at the given level. The
// format is JSON, because these servers run as containers whose output is
// collected rather than read.
func NewLogger(level string, out io.Writer) (*slog.Logger, error) {
	var parsed slog.Level
	switch strings.ToLower(level) {
	case "", "info":
		parsed = slog.LevelInfo
	case "debug":
		parsed = slog.LevelDebug
	case "warn", "warning":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	default:
		return nil, fmt.Errorf("unknown log level %q: must be one of debug|info|warn|error", level)
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}
