// Shared Vitest preset for HMS packages/apps (jsdom + Testing Library).
export function hmsVitest() {
  return {
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["@hms/config/vitest-setup"],
      passWithNoTests: true,
    },
  };
}
