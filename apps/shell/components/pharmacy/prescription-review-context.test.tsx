import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { PrescriptionReviewContext } from "./prescription-review-context";
import { Prescriptions } from "./prescriptions";

const scope = {
  tenantId: "11111111-1111-7111-8111-111111111111",
  subject: "synthetic-pharmacist",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const id = "0199e000-0000-7000-8000-000000000040";
const context = {
  owner_tenant_id: scope.tenantId,
  reader_subject: scope.subject,
  pharmacy_id: scope.pharmacyId,
  source_schema_version: 1,
  source_kind: "recorded_prescription",
  source_authenticity: "not_evaluated",
  source_state_hash: "a".repeat(64),
  clinical_authority: "not_evaluated",
  clinical_context: {
    allergies: "unavailable",
    active_medications: "unavailable",
    patient_age: "unavailable",
    patient_weight: "unavailable",
    clinical_flags: "unavailable",
  },
  record: {
    id,
    outlet_id: scope.outletId,
    prescriber_name: "TEST Dr Asha Rao",
    prescriber_registration: "QA/123",
    prescriber_council: "TEST Council",
    prescribed_on: "2026-11-25",
    patient_name: "TEST Ravi Kumar",
    patient_address: null,
    prescriber_address: "TEST Clinic Lane",
    recorded_by: "synthetic-recorder-id",
    created_at: "2026-11-26T00:00:00Z",
    lines: [
      {
        line_number: 1,
        prescribed_as: "TEST medicine as written",
        product_id: "0199e000-0000-7000-8000-000000000003",
        quantity_units: 20,
        dispensings_allowed: 2,
        units_remaining: 15,
        dispensings_remaining: 1,
      },
    ],
  },
};

afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("shows the authorized recorded copy and unknown clinical facts without claiming approval", async () => {
  const fetcher = vi.fn(async () => Response.json(context));
  vi.stubGlobal("fetch", fetcher);
  const close = vi.fn();
  const { user } = renderWithProviders(
    <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={close} />,
  );
  expect(
    await screen.findByRole("heading", { name: "Recorded prescription for TEST Ravi Kumar" }),
  ).toHaveFocus();
  expect(screen.getByText("TEST Dr Asha Rao")).toBeVisible();
  expect(screen.getByText("TEST Council · QA/123")).toBeVisible();
  expect(screen.getByText("TEST medicine as written")).toBeVisible();
  expect(screen.getByText("15 of 20 units remain in the recorded allowance.")).toBeVisible();
  expect(screen.getByText("1 of 2 recorded dispensings remain.")).toBeVisible();
  expect(screen.getByText("Patient address not recorded.")).toBeVisible();
  expect(screen.getByRole("note")).toHaveTextContent(/does not approve supply/i);
  for (const label of [
    "Allergies",
    "Active medicines",
    "Age",
    "Weight",
    "Clinical and schedule flags",
  ]) {
    expect(screen.getByText(label).nextElementSibling).toHaveTextContent("Unavailable");
  }
  expect(screen.queryByRole("button", { name: /^approve/i })).not.toBeInTheDocument();
  expect(document.body).not.toHaveTextContent(context.record.recorded_by);
  expect(document.body).not.toHaveTextContent(scope.outletId);
  expect(document.body).not.toHaveTextContent(context.source_state_hash);
  const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toContain(`/prescriptions/${id}/review-context`);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(init.cache).toBe("no-store");
  await user.click(screen.getByRole("button", { name: "Close prescription review" }));
  expect(close).toHaveBeenCalledOnce();
});

it("opens the review from the recorded list and returns keyboard focus on close", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      Response.json(
        url.endsWith("/review-context")
          ? context
          : {
              data: [context.record],
              page: { has_more: false, next_cursor: null },
            },
      ),
    ),
  );
  const { user } = renderWithProviders(
    <Prescriptions scope={scope} canRecord={false} reviewSubject={scope.subject} />,
  );
  const trigger = await screen.findByRole("button", {
    name: "Review recorded details for TEST Ravi Kumar",
  });
  await user.click(trigger);
  expect(
    await screen.findByRole("heading", { name: /Recorded prescription for TEST Ravi Kumar/ }),
  ).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Close prescription review" }));
  expect(trigger).toHaveFocus();
  expect(
    screen.queryByRole("heading", { name: /Recorded prescription for TEST Ravi Kumar/ }),
  ).not.toBeInTheDocument();
});

it("discards the sensitive query after closing and aborts an unfinished read", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const fetcher = vi.fn(async () => Response.json(context));
  vi.stubGlobal("fetch", fetcher);
  const { unmount } = render(
    <QueryClientProvider client={client}>
      <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  await screen.findByRole("heading", { name: /TEST Ravi Kumar/ });
  unmount();
  await waitFor(() => expect(client.getQueryCache().getAll()).toHaveLength(0));

  const pending = vi.fn<typeof fetch>(() => new Promise<Response>(() => {}));
  vi.stubGlobal("fetch", pending);
  const reading = render(
    <QueryClientProvider client={client}>
      <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(pending).toHaveBeenCalledOnce());
  const signal = pending.mock.calls[0][1]?.signal;
  expect(signal?.aborted).toBe(false);
  reading.unmount();
  expect(signal?.aborted).toBe(true);
  await waitFor(() => expect(client.getQueryCache().getAll()).toHaveLength(0));
});

it.each([403, 404, 503])(
  "keeps clinical data hidden on a %s response and can retry",
  async (status) => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ error: "unavailable" }, { status }))
      .mockImplementation(async () => Response.json(context));
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(
      <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={vi.fn()} />,
    );
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.queryByText("TEST Ravi Kumar")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: status === 503 ? "Try again" : "Recheck access" }),
    );
    expect(await screen.findByRole("heading", { name: /TEST Ravi Kumar/ })).toBeVisible();
  },
);

it("hides the previous context throughout refresh and after a refresh failure", async () => {
  let finish: (response: Response) => void = () => {
    throw new Error("refresh was not requested");
  };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(Response.json(context))
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={vi.fn()} />,
  );
  await screen.findByRole("heading", { name: /TEST Ravi Kumar/ });
  await user.click(screen.getByRole("button", { name: "Refresh recorded details" }));
  expect(screen.getByRole("status")).toHaveTextContent("Loading recorded prescription");
  expect(screen.queryByText("TEST Dr Asha Rao")).not.toBeInTheDocument();
  await act(async () => finish(Response.json({ error: "unavailable" }, { status: 503 })));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByText("TEST medicine as written")).not.toBeInTheDocument();
});

it("clears the review on a tenant transition and requires reopening even if switching is cancelled", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(context)),
  );
  renderWithProviders(
    <PrescriptionReviewContext scope={scope} prescriptionId={id} onClose={vi.fn()} />,
  );
  await screen.findByRole("heading", { name: /TEST Ravi Kumar/ });
  act(() => beginTenantTransition());
  expect(screen.getByRole("status")).toHaveTextContent(/workspace changed/i);
  expect(screen.queryByText("TEST Dr Asha Rao")).not.toBeInTheDocument();
  act(() => cancelTenantTransition());
  expect(screen.getByRole("status")).toHaveTextContent(/reopen/i);
  expect(screen.queryByText("TEST Dr Asha Rao")).not.toBeInTheDocument();
});

it("does not display a late previous-account response or a foreign scope response", async () => {
  let finish: (response: Response) => void = () => {
    throw new Error("first read was not requested");
  };
  const fetcher = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    )
    .mockImplementation(async () => Response.json(context));
  vi.stubGlobal("fetch", fetcher);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const panel = (reader: string) => (
    <QueryClientProvider client={client}>
      <PrescriptionReviewContext
        scope={{ ...scope, subject: reader }}
        prescriptionId={id}
        onClose={vi.fn()}
      />
    </QueryClientProvider>
  );
  const { rerender } = render(panel(scope.subject));
  await waitFor(() => expect(fetcher).toHaveBeenCalledOnce());
  rerender(panel("other-pharmacist"));
  await act(async () => finish(Response.json(context)));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByText("TEST Dr Asha Rao")).not.toBeInTheDocument();
});
