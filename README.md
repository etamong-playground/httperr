# httperr — etamong-lab cross-app error convention

This repo holds the convention in **two stacks** (same contract, same log line):

- **Go** (this README): `go get gitlab.com/etamong-lab/shared/libs/httperr` — see below.
- **TypeScript/JS**: `@etamong-lab/httperr`, published from [`ts/`](ts/) (Node, Cloudflare
  Workers, browser). Use `fail()` server-side and `formatError()` in frontends.

---

The Go library, for Go HTTP services. A handler reports
a failure once; the helper writes a clean, non-leaky `{"error","ref"}` response to the
client **and** logs one structured JSON record server-side under the same 8-hex `ref`.

The `ref` is the **join key** between a user's report and the exact log line: paste it
into the "etamong-lab Errors" Grafana dashboard and it resolves across every app. The
log record is JSON so Loki parses it with `| json` and aggregates identically across
services. See [planning#188](https://gitlab.com/etamong-lab/planning/-/issues/188) and
the wiki concept `cross-app-error-view`.

## Install

```sh
go get gitlab.com/etamong-lab/shared/libs/httperr
```

Private module — consumers set `GOPRIVATE=gitlab.com/etamong-lab/*` and have git
credentials for gitlab.com.

## Use

Construct one `Responder` at startup and share it across handlers:

```go
resp := &httperr.Responder{
    Log:  httperr.NewLogger(os.Stdout),                 // JSON, lowercase level
    App:  "pages",                                       // = k8s app / namespace
    User: func(r *http.Request) string {                 // optional caller identity
        return identityFrom(r.Context()).Email
    },
    // Route is optional; defaults to r.Pattern (Go 1.23+) then r.URL.Path.
}

func handler(w http.ResponseWriter, r *http.Request) {
    if err := doThing(); err != nil {
        resp.Fail(w, r, http.StatusBadRequest, "요청을 처리하지 못했어요.", err)
        return
    }
}
```

`Fail` returns the `ref`. For streaming responses where headers are already flushed,
use `Ref` to log + get a ref without writing a body, then splice it in yourself.

## The contract

Response body (always): `{"error":"<clean localized message>","ref":"<8 hex>"}`.
The raw `err` is **never** sent to the client.

Log record (one JSON line to stdout):

```json
{"time":"…","level":"error","msg":"request failed","app":"pages","ref":"3f9a1c0b","method":"POST","path":"/api/v1/sites/{slug}/deploys","status":400,"user":"to.jooholee@gmail.com","err":"multipart: message too large"}
```

`ref` is a parsed field, **never** a Loki stream label (cardinality). `path` should be
a route template, not the raw URL. Mirror this exact key set in other languages
(`@etamong-lab/httperr` for TS, a `tracing` snippet for Rust) so a quoted ref maps 1:1
to a Loki lookup in any app.
