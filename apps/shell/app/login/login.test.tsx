import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { SIGNED_OUT_MARK } from "@hms/ui";

import LoginPage from "./page";

const signinRedirect = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

describe("LoginPage", () => {
  beforeEach(() => {
    signinRedirect.mockReset();
    getUserManager.mockReturnValue({ signinRedirect });
    window.sessionStorage.clear();
  });

  // Arriving here unauthenticated is the COMMON case — middleware.ts
  // redirects any unauthenticated request to /login — so the default
  // wording must not claim a sign-out happened. Saying "You are signed
  // out" to someone who was never signed in is untrue, and on a shared
  // terminal it implies the previous clinician's session was ended when
  // nothing of the sort occurred.
  it("does not claim the user signed out when they simply arrived here", () => {
    render(<LoginPage />);
    expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
    expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
  });

  it("confirms the sign-out when the user actually signed out", async () => {
    window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
    render(<LoginPage />);
    expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
  });

  // Consumed on read: a reload, or navigating back here later in the same
  // tab, is no longer "you just signed out".
  it("only confirms it once", async () => {
    window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
    const first = render(<LoginPage />);
    expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
    first.unmount();

    render(<LoginPage />);
    expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
  });

  // #847: the redirect must NOT fire on mount. Sign-out lands on this page
  // (it is the registered post_logout_redirect_uri), and an authorization
  // request started on arrival raced Zitadel's session teardown — after
  // which signing in as a different user failed with "User not found in
  // the system". This is the assertion that fails if anyone reinstates the
  // useEffect, and its failure mode is a ward terminal the next clinician
  // cannot sign in to.
  it("does not start an authorization request on mount", async () => {
    render(<LoginPage />);
    // Give any stray effect a chance to run before asserting absence.
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument());
    expect(signinRedirect).not.toHaveBeenCalled();
  });

  // Design spec D5a: HMS renders no credential surface of its own. Zitadel
  // owns passwords, MFA and lockout, so a compromised HMS frontend has no
  // password to harvest.
  it("renders no credential form", () => {
    render(<LoginPage />);
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it("starts the redirect when the user asks for it", async () => {
    signinRedirect.mockResolvedValue(undefined);
    render(<LoginPage />);

    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(signinRedirect).toHaveBeenCalledTimes(1));
  });

  // prompt=login belongs on THIS request and nowhere else. Silent renewal
  // (lib/renew.ts) uses prompt=none; forcing re-authentication there would
  // make every background refresh demand a password mid-consultation. And
  // omitting it here would let a live Zitadel session answer silently for
  // whoever is now at the keyboard, which is the whole point of the button.
  it("forces re-authentication rather than accepting a live IdP session", async () => {
    signinRedirect.mockResolvedValue(undefined);
    render(<LoginPage />);

    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() =>
      expect(signinRedirect).toHaveBeenCalledWith(expect.objectContaining({ prompt: "login" })),
    );
  });

  it("shows an error instead of a dead button when the redirect cannot start", async () => {
    signinRedirect.mockRejectedValue(new Error("Zitadel unreachable"));
    render(<LoginPage />);

    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Zitadel unreachable");
    // Re-enabled, so a transient failure does not strand the user on a page
    // whose only control is dead.
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });
});
