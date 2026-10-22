import { render } from "@testing-library/react";
import { act } from "react";
import { toast } from "sonner";
import { afterEach, describe, expect, it } from "vitest";
import { AppProviders } from "./providers";

// sonner's Toaster only renders its [data-sonner-toaster] element once at
// least one toast exists (see sonner/dist/index.mjs — `if
// (!filteredToasts.length) return null`), and the toast store notifies
// subscribers on a timer tick rather than synchronously, so each assertion
// fires a toast and lets a macrotask flush before inspecting the DOM.
async function fireToastAndFlush() {
  await act(async () => {
    toast("theme check");
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("AppProviders toaster", () => {
  afterEach(() => {
    document.documentElement.dataset.theme = "";
  });

  // The app is light only (#1255, spec D1): a stale dark attribute must not
  // turn the toasts dark.
  it("renders the Toaster light even under a dark data-theme", async () => {
    document.documentElement.dataset.theme = "dark";
    render(<AppProviders>{null}</AppProviders>);
    await fireToastAndFlush();
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "light");
  });
});
