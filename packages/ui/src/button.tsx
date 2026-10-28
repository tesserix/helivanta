"use client";

// @tesserix/web's entry is a "use client" module built from `export *`, which a
// server component cannot import names through (they resolve to undefined).
// An explicit re-export gives server pages, such as the public pages, the one
// Button (#1434).
export { Button } from "@tesserix/web";
