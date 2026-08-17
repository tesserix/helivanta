// Shared Vitest preset for HMS packages/apps (jsdom + Testing Library).
export function hmsVitest() {
  return {
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["@helivanta/config/vitest-setup"],
      passWithNoTests: true,
      server: {
        deps: {
          inline: ["@tesserix/web"],
        },
      },
    },
  };
}
