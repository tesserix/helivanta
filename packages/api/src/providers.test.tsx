import { render } from "@testing-library/react";
import { act } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
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

describe("AppProviders theme-aware toaster", () => {
  beforeEach(() => {
    document.documentElement.dataset.theme = "default";
  });

  afterEach(() => {
    document.documentElement.dataset.theme = "";
  });

  it("renders the Toaster in light mode when data-theme is default", async () => {
    render(<AppProviders>{null}</AppProviders>);
    await fireToastAndFlush();
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "light");
  });

  it("renders the Toaster in dark mode when data-theme is dark", async () => {
    document.documentElement.dataset.theme = "dark";
    await act(async () => {
      render(<AppProviders>{null}</AppProviders>);
    });
    await fireToastAndFlush();
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "dark");
  });

  it("flips the Toaster theme when document.documentElement.dataset.theme mutates", async () => {
    await act(async () => {
      render(<AppProviders>{null}</AppProviders>);
    });
    await fireToastAndFlush();
    let toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "light");

    await act(async () => {
      document.documentElement.dataset.theme = "dark";
      // Let the MutationObserver callback flush.
      await Promise.resolve();
    });

    toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "dark");
  });
});
