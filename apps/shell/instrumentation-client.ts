import { z } from "zod";
import { installClientErrorReporting } from "./lib/frontend-errors-client";

// Run before application code: schema compilation must respect the shell's CSP.
z.config({ jitless: true });
installClientErrorReporting();
