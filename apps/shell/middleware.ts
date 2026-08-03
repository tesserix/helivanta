import { NextRequest, NextResponse } from "next/server";

const PUBLIC_PATHS = ["/login", "/api/session"];

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (PUBLIC_PATHS.some((p) => pathname.startsWith(p))) {
    return NextResponse.next();
  }
  if (!req.cookies.get("hms_session")?.value) {
    if (pathname.startsWith("/api/")) {
      return NextResponse.json(
        { error: "unauthenticated", message: "missing credentials" },
        { status: 401 }
      );
    }
    const login = new URL("/login", req.url);
    return NextResponse.redirect(login);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
