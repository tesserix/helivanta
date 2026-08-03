import { NextRequest, NextResponse } from "next/server";

const SESSION_COOKIE = "hms_session";
const MAX_AGE_SECONDS = 60 * 60; // GIP ID tokens live 1h

export async function POST(req: NextRequest) {
  const contentType = req.headers.get("content-type") ?? "";
  const fetchSite = req.headers.get("sec-fetch-site");
  if (
    !contentType.toLowerCase().startsWith("application/json") ||
    (fetchSite && fetchSite !== "same-origin")
  ) {
    return NextResponse.json({ error: "invalid_request" }, { status: 403 });
  }

  let idToken: unknown;
  try {
    ({ idToken } = await req.json());
  } catch {
    return NextResponse.json({ error: "invalid_request" }, { status: 400 });
  }
  if (typeof idToken !== "string" || idToken.length < 10) {
    return NextResponse.json({ error: "invalid_request" }, { status: 400 });
  }
  // The Go API verifies the token cryptographically on every request
  // (pkg/authn); this handler only sets the HttpOnly transport cookie.
  const res = NextResponse.json({ ok: true });
  res.cookies.set(SESSION_COOKIE, idToken, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: MAX_AGE_SECONDS,
  });
  return res;
}
