import { expect, it } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";
it("allows published branding before login but protects workspace settings", () => {
  expect(
    middleware(
      new NextRequest(
        "https://helivanta.app/api/v1/auth/branding/11111111-1111-4111-8111-111111111111",
      ),
    ).status,
  ).toBe(200);
  expect(
    middleware(new NextRequest("https://helivanta.app/api/v1/workspace/branding")).status,
  ).toBe(401);
});
