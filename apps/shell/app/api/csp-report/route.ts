// Content-Security-Policy violation reports (report-uri, #1113 spec D6). Public:
// violations happen on /login before any session exists. Unauthenticated, so
// each request is bounded (16 KiB read, one fixed-shape log line); throttling
// is #861's.

const MAX_BODY_BYTES = 16 * 1024;
const MAX_FIELD_CHARS = 512;

const STRING_FIELDS = ["effective-directive", "disposition"] as const;
const URL_FIELDS = ["document-uri", "blocked-uri", "source-file"] as const;
const NUMBER_FIELDS = ["line-number", "column-number"] as const;

export async function POST(request: Request): Promise<Response> {
  if (request.headers.get("content-type")?.split(";")[0]?.trim() !== "application/csp-report") {
    return new Response(null, { status: 415 });
  }
  const body = await readBounded(request);
  if (body === undefined) return new Response(null, { status: 413 });

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return new Response(null, { status: 400 });
  }
  const violation = isObject(parsed) ? parsed["csp-report"] : undefined;
  if (!isObject(violation)) return new Response(null, { status: 400 });

  const line: Record<string, string | number> = { msg: "csp violation" };
  for (const field of STRING_FIELDS) {
    const value = violation[field];
    if (typeof value === "string") line[field] = value.slice(0, MAX_FIELD_CHARS);
  }
  for (const field of URL_FIELDS) {
    const value = violation[field];
    if (typeof value === "string") line[field] = withoutQuery(value).slice(0, MAX_FIELD_CHARS);
  }
  for (const field of NUMBER_FIELDS) {
    const value = violation[field];
    if (typeof value === "number") line[field] = value;
  }
  console.warn(JSON.stringify(line));
  return new Response(null, { status: 204 });
}

// Reads at most MAX_BODY_BYTES, whatever Content-Length claims; undefined past it.
async function readBounded(request: Request): Promise<string | undefined> {
  if (!request.body) return "";
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > MAX_BODY_BYTES) {
      await reader.cancel();
      return undefined;
    }
    chunks.push(value);
  }
  return new TextDecoder().decode(Buffer.concat(chunks));
}

// Query strings and fragments carry search terms such as patient names, so only
// origin and path are logged. Non-URL values are kept only as CSP keywords
// ("inline", "eval").
function withoutQuery(value: string): string {
  if (/^[a-z-]+$/.test(value)) return value;
  try {
    const url = new URL(value);
    if (url.protocol === "https:" || url.protocol === "http:") return url.origin + url.pathname;
  } catch {
    // not a URL
  }
  return "invalid";
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
