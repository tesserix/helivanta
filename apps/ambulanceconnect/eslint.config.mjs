import { defineConfig } from "eslint/config";
import expoConfig from "eslint-config-expo/flat.js";
export default defineConfig([
  expoConfig,
  { ignores: ["dist/**", ".expo/**", "coverage/**"] },
  { files: ["jest.setup.cjs"], languageOptions: { globals: { jest: "readonly" } } },
  {
    files: ["metro.config.cjs"],
    languageOptions: { globals: { __dirname: "readonly", __filename: "readonly" } },
  },
]);
