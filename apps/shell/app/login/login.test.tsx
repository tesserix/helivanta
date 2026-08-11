import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { PERMISSIONS_CACHE_KEY } from "@hms/api";
import LoginPage from "./page";

const signInWithEmailAndPassword = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
}));
vi.mock("firebase/auth", () => ({ signInWithEmailAndPassword }));
vi.mock("@/lib/firebase", () => ({ firebaseAuth: () => ({}) }));

describe("LoginPage", () => {
  beforeEach(() => {
    window.localStorage.clear();
    signInWithEmailAndPassword.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("prefills dev credentials and validates inline", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    expect(screen.getByLabelText("Email")).toHaveValue("test@hms.dev");
    await user.clear(screen.getByLabelText("Email"));
    await user.clear(screen.getByLabelText("Password"));
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  });

  // A session that expired without an explicit sign-out leaves the
  // previous user's cached permissions behind. The next person signing in
  // on the same browser must not paint the old user's nav, so a
  // successful sign-in clears the cache too — not only the logout link.
  it("clears the cached permissions on successful sign-in", async () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({
        subject: "old-user",
        tenantId: "t1",
        permissions: ["lab.order.read"],
        storedAt: Date.now(),
      }),
    );
    signInWithEmailAndPassword.mockResolvedValue({
      user: { getIdToken: async () => "fresh-id-token" },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ ok: true }) }),
    );

    const user = userEvent.setup();
    render(<LoginPage />);
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
  });
});
