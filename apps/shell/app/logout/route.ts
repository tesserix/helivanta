import { NextRequest, NextResponse } from "next/server";

const SESSION_COOKIE = "hms_session";
const API_URL = process.env.API_URL ?? "http://localhost:8080";

/**
 * POST, not GET, and same-origin checked — the same `sec-fetch-site` check
 * `/api/session` already uses.
 *
 * While logout only cleared a cookie, CSRF against it was an annoyance:
 * `<img src="/logout">` logged someone out and they signed back in. Now
 * that it revokes every session for the subject
 * (`backend/internal/modules/iam/signout.go`, #781), the same trivial
 * attack is a remote denial of service against a clinician mid-shift.
 * `packages/ui/src/hms-shell.tsx` renders sign-out as a form for the same
 * reason; see docs/standards/frontend.md §6 for the documented exception.
 */
export async function POST(req: NextRequest) {
  const fetchSite = req.headers.get("sec-fetch-site");
  if (fetchSite && fetchSite !== "same-origin") {
    return NextResponse.json({ error: "invalid_request" }, { status: 403 });
  }

  // Server-side revocation first. If this fails the session stays live,
  // and telling the caller they are signed out when they are not is the
  // failure this endpoint exists to prevent — the cookie below is only
  // ever cleared once the API call has actually succeeded.
  const session = req.cookies.get(SESSION_COOKIE)?.value;
  if (session) {
    let res: Response;
    try {
      res = await fetch(`${API_URL}/v1/iam/me/sign-out`, {
        method: "POST",
        headers: { Authorization: `Bearer ${session}` },
      });
    } catch {
      return NextResponse.json({ error: "sign_out_failed" }, { status: 502 });
    }
    if (!res.ok) {
      return NextResponse.json({ error: "sign_out_failed" }, { status: 502 });
    }
  }

  const out = NextResponse.json({ ok: true });
  out.cookies.set(SESSION_COOKIE, "", { path: "/", maxAge: 0 });
  return out;
}
