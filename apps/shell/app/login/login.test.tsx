import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import LoginPage from "./page";

const signinRedirect = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

describe("LoginPage", () => {
  beforeEach(() => {
    signinRedirect.mockReset();
    getUserManager.mockReturnValue({ signinRedirect });
  });

  // Design spec D5a: HMS renders no login form. The page's entire job is
  // triggering the redirect — this is the load-bearing behavioural
  // difference from the deleted Firebase form, which rendered
  // email/password fields and never called anything named "redirect".
  it("triggers the Zitadel redirect on mount and renders no credential form", async () => {
    signinRedirect.mockResolvedValue(undefined);
    render(<LoginPage />);

    await waitFor(() => expect(signinRedirect).toHaveBeenCalledTimes(1));
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it("shows an error instead of a blank page when the redirect itself cannot start", async () => {
    signinRedirect.mockRejectedValue(new Error("Zitadel unreachable"));
    render(<LoginPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Zitadel unreachable");
  });
});
