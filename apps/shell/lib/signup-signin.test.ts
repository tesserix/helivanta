import { afterEach, expect, it, vi } from "vitest";

const signinRedirect = vi.hoisted(() => vi.fn(async () => {}));
vi.mock("./oidc", () => ({ getUserManager: () => ({ signinRedirect }) }));

import { signinAfterStaffSetup } from "./signup-signin";

afterEach(() => window.sessionStorage.clear());

// #1778: the staff page marks the tab so /login asks to resume, and the
// unscoped email card resolves the account after the parked password.
it("marks the tab for resume and starts an unscoped, forced sign-in", async () => {
  await signinAfterStaffSetup();
  expect(window.sessionStorage.getItem("helivanta.setupResume")).toBe("1");
  expect(signinRedirect).toHaveBeenCalledWith({
    state: { purpose: "email-login" },
    prompt: "login",
    max_age: 0,
  });
});
