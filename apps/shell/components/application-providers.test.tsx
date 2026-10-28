import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SESSION_CHANGE_KEY } from "@helivanta/api";
import { ApplicationProviders } from "./application-providers";

const usePathname = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({ usePathname }));

beforeEach(() => vi.stubGlobal("location", { replace: vi.fn() }));
afterEach(() => vi.unstubAllGlobals());

function signalOtherTab() {
  act(() =>
    window.dispatchEvent(
      new StorageEvent("storage", {
        key: SESSION_CHANGE_KEY,
        newValue: "synthetic-other-tab",
        storageArea: window.localStorage,
      }),
    ),
  );
}

it("keeps an anonymous ambulance draft when another tab changes authentication", () => {
  usePathname.mockReturnValue("/ambulance");
  render(
    <ApplicationProviders>
      <input aria-label="Synthetic public draft" defaultValue="Keep this draft" />
    </ApplicationProviders>,
  );
  signalOtherTab();
  expect(screen.getByLabelText("Synthetic public draft")).toHaveValue("Keep this draft");
  expect(window.location.replace).not.toHaveBeenCalled();
});

it("removes a private Staff form when another tab changes authentication", () => {
  usePathname.mockReturnValue("/workforce");
  render(
    <ApplicationProviders>
      <input aria-label="Synthetic staff detail" defaultValue="Private draft" />
    </ApplicationProviders>,
  );
  signalOtherTab();
  expect(screen.queryByLabelText("Synthetic staff detail")).not.toBeInTheDocument();
  expect(window.location.replace).toHaveBeenCalledExactlyOnceWith("/login");
});
