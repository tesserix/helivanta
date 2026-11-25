import { RELEASE_POLICY_VERSION, releaseDefinitions } from "./release-capabilities-contract";

export {
  RELEASE_POLICY_VERSION as releasePolicyVersionForTest,
  releaseDefinitions as releaseDefinitionsForTest,
  releasePages as releasePagesForTest,
} from "./release-capabilities-contract";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster, toast } from "sonner";
import type { ReactElement } from "react";

// Component-test harness: fresh query client (no retries), toast outlet,
// and a wired-up user-event instance.
export function renderWithProviders(ui: ReactElement) {
  // sonner keeps toasts module-wide and replays active ones to a new Toaster, so clear the previous test's.
  toast.dismiss();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const user = userEvent.setup();
  const result = render(
    <QueryClientProvider client={client}>
      {ui}
      <Toaster />
    </QueryClientProvider>,
  );
  return { ...result, user };
}

export function releaseSnapshotForTest(
  scope: { tenantId: string; subject: string },
  keys: readonly string[] = releaseDefinitions.map(({ key }) => key),
) {
  const now = Date.now();
  return {
    tenant_id: scope.tenantId,
    subject: scope.subject,
    policy_version: RELEASE_POLICY_VERSION,
    issued_at: new Date(now).toISOString(),
    expires_at: new Date(now + 60_000).toISOString(),
    capabilities: releaseDefinitions.map(({ key }) => ({ key, available: keys.includes(key) })),
  };
}
