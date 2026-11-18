import { readBoundedBody } from "@/lib/bounded-body";
import { CLIENT_ERROR_EVENT, frontendErrorReportContract } from "@/lib/frontend-errors-contract";

const headers = { "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" };
const WINDOW_MS = 60_000;
const MAX_ATTEMPTS = 60;
let windowStart = 0;
let attempts = 0;
const accepted = new Map<string, string>();

function refused(status: number, error = "invalid_report", extra: Record<string, string> = {}) {
  return Response.json(
    { error, message: "The diagnostic report could not be accepted." },
    {
      status,
      headers: { ...headers, ...extra },
    },
  );
}

function sameOrigin(request: Request): boolean {
  try {
    const raw = request.headers.get("origin");
    const host = request.headers.get("host");
    const site = request.headers.get("sec-fetch-site");
    if (!raw || !host || (site !== null && site !== "same-origin")) return false;
    const origin = new URL(raw);
    if (!["http:", "https:"].includes(origin.protocol) || origin.origin !== raw) return false;
    const authority = new URL(`${origin.protocol}//${host}`);
    if (
      authority.username ||
      authority.password ||
      authority.pathname !== "/" ||
      authority.search ||
      authority.hash ||
      authority.origin !== origin.origin
    )
      return false;
    const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname);
    return process.env.NODE_ENV !== "production" || origin.protocol === "https:" || loopback;
  } catch {
    return false;
  }
}

export async function POST(request: Request): Promise<Response> {
  if (!sameOrigin(request)) return refused(403, "origin_refused");
  if (new URL(request.url).search) return refused(400);
  if (
    request.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase() !== "application/json"
  )
    return refused(415);
  const now = Date.now();
  if (now - windowStart >= WINDOW_MS || now < windowStart) {
    windowStart = now;
    attempts = 0;
    accepted.clear();
  }
  if (attempts >= MAX_ATTEMPTS)
    return refused(429, "rate_limited", {
      "Retry-After": String(Math.max(1, Math.ceil((windowStart + WINDOW_MS - now) / 1000))),
      "RateLimit-Limit": String(MAX_ATTEMPTS),
      "RateLimit-Remaining": "0",
      "RateLimit-Reset": String(Math.ceil((windowStart + WINDOW_MS) / 1000)),
    });
  attempts++;
  const body = await readBoundedBody(request, 1024);
  if (!body.ok) return refused(body.status);
  let input: unknown;
  try {
    input = JSON.parse(body.text);
  } catch {
    return refused(400);
  }
  const parsed = frontendErrorReportContract.safeParse(input);
  if (!parsed.success) return refused(400);
  const report = parsed.data;
  const fingerprint = JSON.stringify(report);
  const previous = accepted.get(report.reference);
  if (previous !== undefined)
    return previous === fingerprint
      ? new Response(null, { status: 204, headers })
      : refused(409, "report_conflict");
  try {
    console.error(JSON.stringify({ severity: "ERROR", event: CLIENT_ERROR_EVENT, ...report }));
  } catch {
    return refused(503, "report_unavailable");
  }
  accepted.set(report.reference, fingerprint);
  return new Response(null, { status: 204, headers });
}
