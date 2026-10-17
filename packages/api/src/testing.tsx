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
