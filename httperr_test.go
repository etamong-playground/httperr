package httperr

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var refRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

func TestNewRefShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		ref := NewRef()
		if !refRe.MatchString(ref) {
			t.Fatalf("ref %q is not 8 lowercase hex chars", ref)
		}
		seen[ref] = true
	}
	if len(seen) < 90 {
		t.Fatalf("expected mostly-unique refs, got %d distinct of 100", len(seen))
	}
}

func decodeLog(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(line, &rec); err != nil {
		t.Fatalf("log line is not JSON (%v): %s", err, line)
	}
	return rec
}

func TestFailResponseAndLog(t *testing.T) {
	var buf bytes.Buffer
	h := &Responder{
		Log:  NewLogger(&buf),
		App:  "pages",
		User: func(*http.Request) string { return "to.jooholee@gmail.com" },
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites/foo/deploys", nil)
	req.Pattern = "POST /api/v1/sites/{slug}/deploys"
	rec := httptest.NewRecorder()

	ref := h.Fail(rec, req, http.StatusBadRequest, "업로드를 처리하지 못했어요.", errors.New("multipart: message too large"))

	if !refRe.MatchString(ref) {
		t.Fatalf("returned ref %q invalid", ref)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body not JSON: %v", err)
	}
	if body["error"] != "업로드를 처리하지 못했어요." {
		t.Fatalf("error = %v", body["error"])
	}
	if body["ref"] != ref {
		t.Fatalf("response ref %v != returned ref %v", body["ref"], ref)
	}
	if _, leaked := body["err"]; leaked {
		t.Fatal("raw err must never appear in the response body")
	}

	rec2 := decodeLog(t, buf.Bytes())
	checks := map[string]any{
		"level":  "error",
		"msg":    "request failed",
		"app":    "pages",
		"ref":    ref,
		"method": "POST",
		"path":   "POST /api/v1/sites/{slug}/deploys",
		"user":   "to.jooholee@gmail.com",
		"err":    "multipart: message too large",
	}
	for k, want := range checks {
		if rec2[k] != want {
			t.Errorf("log[%q] = %v, want %v", k, rec2[k], want)
		}
	}
	// status must be a JSON number so `| json | status>=500` works.
	if s, ok := rec2["status"].(float64); !ok || int(s) != http.StatusBadRequest {
		t.Errorf("log status = %v (%T), want 400 as number", rec2["status"], rec2["status"])
	}
}

func TestRefLogsWithoutBody(t *testing.T) {
	var buf bytes.Buffer
	h := &Responder{Log: NewLogger(&buf), App: "pages"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/x/logs", nil)

	ref := h.Ref(req, http.StatusInternalServerError, errors.New("upstream gone"))
	if !refRe.MatchString(ref) {
		t.Fatalf("ref %q invalid", ref)
	}
	rec := decodeLog(t, buf.Bytes())
	if rec["ref"] != ref || rec["status"].(float64) != 500 || rec["user"] != "-" {
		t.Fatalf("unexpected log record: %v", rec)
	}
}

func TestRouteFallback(t *testing.T) {
	h := &Responder{}
	// No Route func, no Pattern -> URL.Path.
	req := httptest.NewRequest(http.MethodGet, "/raw/path", nil)
	if got := h.route(req); got != "/raw/path" {
		t.Errorf("route fallback = %q, want /raw/path", got)
	}
	// Pattern present -> Pattern wins over URL.Path.
	req.Pattern = "GET /raw/{x}"
	if got := h.route(req); got != "GET /raw/{x}" {
		t.Errorf("route = %q, want pattern", got)
	}
	// Explicit Route func wins over everything.
	h.Route = func(*http.Request) string { return "tmpl" }
	if got := h.route(req); got != "tmpl" {
		t.Errorf("route = %q, want tmpl", got)
	}
}

func TestNilUserAndErr(t *testing.T) {
	var buf bytes.Buffer
	h := &Responder{Log: NewLogger(&buf), App: "x"}
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	rec := httptest.NewRecorder()
	h.Fail(rec, req, http.StatusForbidden, "nope", nil) // nil err
	got := decodeLog(t, buf.Bytes())
	if got["user"] != "-" || got["err"] != "" {
		t.Fatalf("nil user/err handling: %v", got)
	}
}
