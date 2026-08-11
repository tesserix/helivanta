"use client";

import { useEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";

export const THEME_STORAGE_KEY = "hms.theme";

// Minified, dependency-free IIFE — inlined as a raw <script> in every app's
// <body> (see apps/*/app/layout.tsx), not run as a React effect. Effects run
// after first paint, so a state-driven data-theme would draw the light theme
// first and swap to dark a frame later for users who prefer dark. This
// script stamps `data-theme` synchronously before the browser paints
// anything, matching what ThemeToggle reads on mount. Wrapped in try/catch
// because localStorage/matchMedia can throw (private browsing, disabled
// storage) — on failure it silently leaves the SSR default ("default") in
// place rather than breaking the page.
export const THEME_INIT_SCRIPT = `(function(){try{var k="${THEME_STORAGE_KEY}";var s=localStorage.getItem(k);var dark=s==="dark"||(!s&&window.matchMedia("(prefers-color-scheme: dark)").matches);document.documentElement.dataset.theme=dark?"dark":"default";}catch(e){}})();`;

/**
 * 32px icon button that flips `document.documentElement.dataset.theme`
 * between "default" (light) and "dark" and persists the choice to
 * localStorage. Initial state is read from the DOM on mount so it matches
 * whatever THEME_INIT_SCRIPT already stamped before hydration — starting
 * from a hardcoded default here would flash the wrong icon for one frame.
 */
export function ThemeToggle() {
  const [isDark, setIsDark] = useState(false);

  useEffect(() => {
    setIsDark(document.documentElement.dataset.theme === "dark");
  }, []);

  const toggle = () => {
    const next = !isDark;
    document.documentElement.dataset.theme = next ? "dark" : "default";
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, next ? "dark" : "light");
    } catch {
      // localStorage unavailable — theme still applies for this page load.
    }
    setIsDark(next);
  };

  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={isDark ? "Switch to light theme" : "Switch to dark theme"}
      className="flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      {isDark ? (
        <Sun className="h-4 w-4" aria-hidden="true" />
      ) : (
        <Moon className="h-4 w-4" aria-hidden="true" />
      )}
    </button>
  );
}
