// Package httperr is the etamong-lab cross-app error convention. A handler reports
// a failure once and the helper does two things under a single 8-hex reference id:
// it writes a clean, non-leaky {"error","ref"} JSON body to the client, and it logs
// one structured slog record carrying the technical detail server-side. The ref is
// the join key between a user's report and the exact log line — paste it into the
// "etamong-lab Errors" Grafana dashboard to resolve it across every app.
//
// The log record is emitted as JSON so Loki parses it with `| json` and aggregates
// it identically across services (level, app, ref, method, path, status, user, err).
// ref is a parsed field, never a stream label, to keep Loki cardinality bounded.
//
// See planning#188 and the wiki concept cross-app-error-view for the full design.
package httperr

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Responder emits the standard error response and log line for one service.
// Construct one per app at startup and share it across handlers.
type Responder struct {
	// Log receives the structured error record. If nil, slog.Default() is used.
	// Use NewLogger so the JSON shape (notably a lowercase level) matches every
	// other etamong-lab app.
	Log *slog.Logger

	// App is the service name emitted as the "app" field; it should equal the
	// app's k8s app label / namespace so cross-app dashboard filters line up.
	App string

	// User extracts the caller identity for the "user" field (e.g. an email).
	// If nil, or if it returns "", "user" is logged as "-".
	User func(*http.Request) string

	// Route returns the low-cardinality route template for the "path" field. If
	// nil it falls back to r.Pattern (Go 1.23+ ServeMux) then r.URL.Path. Prefer a
	// template like /api/v1/sites/{slug} over the raw path to bound cardinality.
	Route func(*http.Request) string
}

// NewLogger returns the canonical slog.Logger for the convention: a JSON handler
// whose level value is lowercased ("error" not "ERROR") so a single Loki query
// `| json | level="error"` matches uniformly across every app.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if lv, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(strings.ToLower(lv.String()))
				}
			}
			return a
		},
	}))
}

// NewRef returns a short, user-quotable reference id (8 hex chars from 4 random
// bytes). It is a correlation token, not a security or uniqueness guarantee; the
// crypto/rand error is intentionally ignored — a partially-filled ref is still a
// findable token in the logs.
func NewRef() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Fail writes the standard error response to the client and logs the technical
// detail server-side under a fresh ref. userMsg is the clean, localized message
// shown to the user; err is the raw internal error and is never sent to the client
// (pass nil if there is no underlying error). Fail returns the generated ref.
func (h *Responder) Fail(w http.ResponseWriter, r *http.Request, code int, userMsg string, err error) string {
	ref := h.emit(r, code, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": userMsg, "ref": ref})
	return ref
}

// Ref logs the error and returns a fresh ref WITHOUT writing a response body, for
// streaming handlers or any path where headers are already flushed and the caller
// must splice the ref into an already-started response itself.
func (h *Responder) Ref(r *http.Request, code int, err error) string {
	return h.emit(r, code, err)
}

func (h *Responder) emit(r *http.Request, code int, err error) string {
	ref := NewRef()
	logger := h.Log
	if logger == nil {
		logger = slog.Default()
	}
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	logger.Error("request failed",
		"app", h.App,
		"ref", ref,
		"method", r.Method,
		"path", h.route(r),
		"status", code,
		"user", h.who(r),
		"err", errStr,
	)
	return ref
}

func (h *Responder) who(r *http.Request) string {
	if h.User != nil {
		if u := h.User(r); u != "" {
			return u
		}
	}
	return "-"
}

func (h *Responder) route(r *http.Request) string {
	if h.Route != nil {
		if rt := h.Route(r); rt != "" {
			return rt
		}
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.URL.Path
}
