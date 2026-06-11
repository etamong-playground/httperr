// @etamong-lab/httperr — the etamong-lab cross-app error convention for TS/JS.
//
// Mirrors the Go lib (gitlab.com/etamong-lab/shared/libs/httperr): a handler
// reports a failure once and gets back a clean {error, ref} response to send to
// the client, while one structured JSON line is logged under the same 8-hex ref.
// The ref is the join key between a user's report and the exact log line; Loki
// parses the line with `| json`. See planning#188 and the wiki concept
// cross-app-error-view. Framework-agnostic: works in Node, Cloudflare Workers,
// and the browser (Web Crypto + console).

/** The error response envelope sent to clients across every etamong-lab app. */
export interface ErrorResponse {
  error: string;
  ref?: string;
}

/**
 * newRef returns a short, user-quotable reference id (8 hex chars from 4 random
 * bytes). It is a correlation token, not a security or uniqueness guarantee.
 */
export function newRef(): string {
  const b = new Uint8Array(4);
  globalThis.crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

export interface FailContext {
  /** Service name; should equal the app's k8s app/namespace (or pages-apps slug). */
  app: string;
  /** HTTP status to return. 5xx logs at "error", 4xx at "warn". */
  status: number;
  method?: string;
  /** Route template (e.g. /api/v1/sites/{slug}), not the raw URL — bounds cardinality. */
  path?: string;
  /** Caller identity (email or similar); defaults to "-". */
  user?: string;
  /** Raw underlying error — logged, NEVER placed in the client body. */
  err?: unknown;
}

export interface FailResult {
  status: number;
  body: ErrorResponse;
  ref: string;
}

function errString(err: unknown): string {
  if (err == null) return "";
  if (err instanceof Error) return err.message;
  return String(err);
}

/**
 * fail logs the standard etamong-lab JSON error line (to stderr) and returns the
 * client response { status, body: {error, ref} }. It does NOT send the response —
 * the caller does, framework-appropriately:
 *
 *   const r = fail("요청을 처리하지 못했어요.", { app, status: 502, method, path, user, err });
 *   res.status(r.status).json(r.body);                  // Express
 *   return Response.json(r.body, { status: r.status });  // Workers
 *
 * userMsg is the clean, localized message shown to the user; ctx.err is the raw
 * technical error and is only ever logged.
 */
export function fail(userMsg: string, ctx: FailContext): FailResult {
  const ref = newRef();
  const record = {
    level: ctx.status >= 500 ? "error" : "warn",
    msg: "request failed",
    app: ctx.app,
    ref,
    method: ctx.method ?? "",
    path: ctx.path ?? "",
    status: ctx.status,
    user: ctx.user ?? "-",
    err: errString(ctx.err),
  };
  // One JSON line — Loki parses it with `| json` (planning#188).
  console.error(JSON.stringify(record));
  return { status: ctx.status, body: { error: userMsg, ref }, ref };
}

/**
 * formatError turns a server error response — or an axios error wrapping one —
 * into a user-facing string, appending " (ref: xxxx)" when a ref is present so a
 * user can quote it. Decoupled from axios and i18n: it reads {error, ref} off the
 * value (or its axios `.response.data`), and you pass your own fallback string.
 *
 *   <p className="err">{formatError(e, { fallback: t("err.generic") })}</p>
 */
export function formatError(input: unknown, opts?: { fallback?: string }): string {
  const v = input as
    | { error?: string; ref?: string; message?: string; response?: { data?: { error?: string; ref?: string } } }
    | null
    | undefined;
  const data = v?.response?.data ?? v ?? undefined;
  const base = data?.error || v?.message || opts?.fallback || "Request failed";
  return data?.ref ? `${base} (ref: ${data.ref})` : base;
}
