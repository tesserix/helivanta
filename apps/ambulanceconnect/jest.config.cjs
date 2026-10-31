module.exports = {
  preset: "jest-expo",
  clearMocks: true,
  // Cold React Native 0.88 first-render transforms took 14 seconds on CI.
  testTimeout: 30_000,
  moduleNameMapper: {
    "^react$": require.resolve("react"),
    "^react/(.*)$": "<rootDir>/node_modules/react/$1",
  },
  testMatch: ["**/src/**/*.test.tsx"],
  setupFilesAfterEnv: ["./jest.setup.cjs"],
  transformIgnorePatterns: [
    "node_modules/(?!\\.pnpm/|(?:@react-native|@expo|@helivanta)/|(?:react-native|expo|jest-expo|nativewind|react-native-css-interop)(?:/|-))",
  ],
};
