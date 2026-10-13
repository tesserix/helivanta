import { createContext } from "react";
import type { Branding } from "@helivanta/api";

export const BrandThemeContext = createContext<Branding["theme"]>("teal");
