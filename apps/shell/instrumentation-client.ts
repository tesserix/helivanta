import { z } from "zod";

// Run before application code: schema compilation must respect the shell's CSP.
z.config({ jitless: true });
