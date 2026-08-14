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
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Default timeouts. They are deliberately generous: publishing a large ontology
// is the slowest request either server handles.
const (
	// DefaultReadHeaderTimeout bounds how long a client may take to send its
	// request headers.
	DefaultReadHeaderTimeout = 10 * time.Second
	// DefaultWriteTimeout bounds how long a response may take to write.
	DefaultWriteTimeout = 60 * time.Second
	// DefaultShutdownTimeout bounds how long in-flight requests get to finish
	// once shutdown starts.
	DefaultShutdownTimeout = 15 * time.Second
)

// Options configures the HTTP listener.
type Options struct {
	// Address is the host:port to listen on.
	Address string
	// ReadHeaderTimeout bounds reading request headers.
	ReadHeaderTimeout time.Duration
	// WriteTimeout bounds writing a response.
	WriteTimeout time.Duration
	// ShutdownTimeout bounds draining in-flight requests on shutdown.
	ShutdownTimeout time.Duration
}

// SetDefaults fills in the timeouts a caller left unset.
func (o *Options) SetDefaults() {
	if o.ReadHeaderTimeout == 0 {
		o.ReadHeaderTimeout = DefaultReadHeaderTimeout
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = DefaultWriteTimeout
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = DefaultShutdownTimeout
	}
}

// Serve runs handler until ctx is cancelled, then drains in-flight requests.
//
// Draining rather than exiting matters for these servers: a publish that is
// halfway through writing an ontology version should be allowed to commit or
// roll back, not be cut off mid-transaction by a rolling restart.
func Serve(ctx context.Context, handler http.Handler, options Options, logger *slog.Logger) error {
	options.SetDefaults()

	server := &http.Server{
		Addr:              options.Address,
		Handler:           handler,
		ReadHeaderTimeout: options.ReadHeaderTimeout,
		WriteTimeout:      options.WriteTimeout,
	}

	failed := make(chan error, 1)
	go func() {
		logger.Info("listening", "address", options.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
			return
		}
		failed <- nil
	}()

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", options.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), options.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return <-failed
}

// WithLogging logs one line per request. Handlers stay quiet; whether a request
// happened and how it ended is the concern of the layer that sees all of them.
func WithLogging(handler http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		handler.ServeHTTP(recorder, r)

		level := slog.LevelInfo
		if recorder.code >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.Log(r.Context(), level, "request",
			"method", r.Method, "path", r.URL.Path, "status", recorder.code,
			"duration", time.Since(started).Round(time.Millisecond))
	})
}

// RegisterHealth adds the liveness and readiness endpoints.
//
// Liveness answers whether the process is up; readiness answers whether its
// dependencies are, so that a compose or orchestrator restart waits for the
// database rather than sending traffic into a server that cannot serve it.
func RegisterHealth(mux *http.ServeMux, ready func(ctx context.Context) error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, Status{Reason: "OK"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				WriteError(w, http.StatusServiceUnavailable, "NotReady", err)
				return
			}
		}
		WriteJSON(w, http.StatusOK, Status{Reason: "OK"})
	})
}

// statusRecorder remembers the response code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}
