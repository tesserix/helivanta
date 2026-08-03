import { NextResponse } from "next/server";

export async function GET(req: Request) {
  const res = NextResponse.redirect(new URL("/login", req.url));
  res.cookies.set("hms_session", "", { path: "/", maxAge: 0 });
  return res;
}
