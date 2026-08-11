import { act, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { THEME_INIT_SCRIPT, THEME_STORAGE_KEY, ThemeToggle, useThemeAttribute } from "./theme";

function stubMatchMedia(prefersDark: boolean) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: query === "(prefers-color-scheme: dark)" && prefersDark,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  });
}

describe("ThemeToggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.dataset.theme = "default";
  });

  it("renders with the aria-label matching the current dataset.theme (light)", () => {
    document.documentElement.dataset.theme = "default";
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: "Switch to dark theme" })).toBeInTheDocument();
  });

  it("renders with the aria-label matching the current dataset.theme (dark)", () => {
    document.documentElement.dataset.theme = "dark";
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: "Switch to light theme" })).toBeInTheDocument();
  });

  it("flips dataset.theme and persists the choice when clicked", async () => {
    document.documentElement.dataset.theme = "default";
    const { user } = renderWithProviders(<ThemeToggle />);

    await user.click(screen.getByRole("button", { name: "Switch to dark theme" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(screen.getByRole("button", { name: "Switch to light theme" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Switch to light theme" }));
    expect(document.documentElement.dataset.theme).toBe("default");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(screen.getByRole("button", { name: "Switch to dark theme" })).toBeInTheDocument();
  });
});

function ThemeAttributeProbe() {
  const theme = useThemeAttribute();
  return <span data-testid="theme-attribute">{theme}</span>;
}

describe("useThemeAttribute", () => {
  beforeEach(() => {
    document.documentElement.dataset.theme = "default";
  });

  it("reflects the initial data-theme attribute on mount", () => {
    document.documentElement.dataset.theme = "dark";
    renderWithProviders(<ThemeAttributeProbe />);
    expect(screen.getByTestId("theme-attribute")).toHaveTextContent("dark");
  });

  it("stays in sync when data-theme mutates after mount", async () => {
    renderWithProviders(<ThemeAttributeProbe />);
    expect(screen.getByTestId("theme-attribute")).toHaveTextContent("default");

    await act(async () => {
      document.documentElement.dataset.theme = "dark";
      await Promise.resolve();
    });
    expect(screen.getByTestId("theme-attribute")).toHaveTextContent("dark");

    await act(async () => {
      document.documentElement.dataset.theme = "default";
      await Promise.resolve();
    });
    expect(screen.getByTestId("theme-attribute")).toHaveTextContent("default");
  });
});

describe("THEME_INIT_SCRIPT", () => {
  const run = new Function(THEME_INIT_SCRIPT);

  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.dataset.theme = "";
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("stamps dark when localStorage has stored dark", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    stubMatchMedia(false);
    run();
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("stamps default when localStorage has stored light", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "light");
    stubMatchMedia(true);
    run();
    expect(document.documentElement.dataset.theme).toBe("default");
  });

  it("stamps dark when nothing stored and system prefers dark", () => {
    stubMatchMedia(true);
    run();
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("stamps default when nothing stored and system prefers light", () => {
    stubMatchMedia(false);
    run();
    expect(document.documentElement.dataset.theme).toBe("default");
  });
});
