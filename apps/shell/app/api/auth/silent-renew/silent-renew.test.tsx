import { render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import SilentRenewPage from "./page";

const signinSilentCallback = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

describe("SilentRenewPage", () => {
  beforeEach(() => {
    signinSilentCallback.mockReset();
    getUserManager.mockReturnValue({ signinSilentCallback });
  });

  it("completes the silent-renew handshake on mount and renders nothing", () => {
    const { container } = render(<SilentRenewPage />);

    expect(signinSilentCallback).toHaveBeenCalledTimes(1);
    expect(container).toBeEmptyDOMElement();
  });
});
