import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { useState, type ReactElement } from "react";
import { SupplierCreditRecord } from "./supplier-credit-record";

const render = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance-subject-never-display",
};
const source = {
  id: "0199e000-0000-7000-8000-000000000004",
  outlet_id: scope.outletId,
  number: 17,
  payee_name: "TEST Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  currency: "INR",
  expected_credit_minor: 1500,
  matched_minor: 0,
};
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  sessionStorage.clear();
});

it("records an actual supplier note from a named financial source without inferring the expected amount", async () => {
  const writes: Array<Record<string, unknown>> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (url.includes("/source-returns?"))
        return new Response(
          JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
        );
      if (options?.method === "PUT") {
        const input = JSON.parse(options.body as string);
        const receipt = {
          ...input,
          id: url.split("/").at(-1),
          outlet_id: scope.outletId,
          return_number: 17,
          payee_reference: "TEST-SUPPLIER",
          payee_name: source.payee_name,
          original_bill_reference: source.original_bill_reference,
          recorded_by: scope.subject,
          recorded_at: "2026-11-21T00:00:00Z",
        };
        writes.push(receipt);
        return new Response(JSON.stringify(receipt));
      }
      return new Response("TEST unexpected request", { status: 500 });
    }),
  );
  const onRecorded = vi.fn();
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord onRecorded={onRecorded} />);
  await user.click(await screen.findByRole("button", { name: "Record supplier credit" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched supplier return" }));
  await user.click(
    await screen.findByRole("option", { name: /Return 17.*TEST Supplier.*TEST-ORIGINAL-BILL/ }),
  );
  expect(screen.getByLabelText("Actual credit amount (INR)")).toHaveValue("");
  await user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CN-FIRST");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-18" } });
  await user.type(screen.getByLabelText("Actual credit amount (INR)"), "7.50");
  await user.type(
    screen.getByLabelText("Credit record reason"),
    "TEST accepted actual supplier credit",
  );
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  await waitFor(() => expect(onRecorded).toHaveBeenCalledWith(writes[0]?.id));
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    return_id: source.id,
    document_reference: "TEST-CN-FIRST",
    amount_minor: 750,
    currency: "INR",
    correction_of: null,
  });
  expect(document.body).not.toHaveTextContent(scope.subject);
  for (const id of [scope.tenantId, scope.pharmacyId, scope.outletId, source.id])
    expect(document.body).not.toHaveTextContent(id);
});

async function enterCredit(user: ReturnType<typeof render>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Record supplier credit" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched supplier return" }));
  await user.click(await screen.findByRole("option", { name: /Return 17.*TEST Supplier/ }));
  await user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CN-FIRST");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-18" } });
  await user.type(screen.getByLabelText("Actual credit amount (INR)"), "7.50");
  await user.type(
    screen.getByLabelText("Credit record reason"),
    "TEST accepted actual supplier credit",
  );
}
it("keeps the entered actual amount visible after a definite refusal instead of submitting a hidden amount", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) =>
      options?.method === "PUT"
        ? new Response(JSON.stringify({ error: "invalid_request", message: "TEST refusal" }), {
            status: 400,
          })
        : new Response(
            JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
          ),
    ),
  );
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord />);
  await enterCredit(user);
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(await screen.findByText(/Credit was not recorded/)).toBeInTheDocument();
  expect(await screen.findByLabelText("Actual credit amount (INR)")).toHaveValue("7.50");
  expect(sessionStorage.length).toBe(0);
});

function receiptFor(url: string, options: RequestInit) {
  return {
    ...JSON.parse(options.body as string),
    id: url.split("/").at(-1)!,
    outlet_id: scope.outletId,
    return_number: 17,
    payee_reference: "TEST-SUPPLIER",
    payee_name: source.payee_name,
    original_bill_reference: source.original_bill_reference,
    recorded_by: scope.subject,
    recorded_at: "2026-11-21T00:00:00Z",
  };
}
it("recovers a lost response after remount with only a scoped reference and no new writes when recording is disabled", async () => {
  const posted: ReturnType<typeof receiptFor>[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/recovery")) return new Response(JSON.stringify(posted[0]));
    if (options?.method === "PUT") {
      posted.push(receiptFor(url, options));
      throw new TypeError("TEST lost response");
    }
    return new Response(
      JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(<SupplierCreditRecord scope={scope} canRecord />);
  await enterCredit(first.user);
  await first.user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(await screen.findByText(/credit outcome is unconfirmed/)).toBeInTheDocument();
  expect(posted).toHaveLength(1);
  expect(sessionStorage.length).toBe(1);
  expect(sessionStorage.getItem(sessionStorage.key(0)!)).toBe(posted[0].id);
  first.unmount();
  fetcher.mockClear();
  const onRecorded = vi.fn();
  const second = render(
    <SupplierCreditRecord scope={scope} canRecord={false} onRecorded={onRecorded} />,
  );
  await second.user.click(await screen.findByRole("button", { name: "Check original credit" }));
  await waitFor(() => expect(onRecorded).toHaveBeenCalledWith(posted[0].id));
  expect(fetcher.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);
  expect(fetcher.mock.calls.some(([url]) => url.includes("source-returns"))).toBe(false);
  expect(sessionStorage.length).toBe(0);
  expect(document.body).not.toHaveTextContent(scope.subject);
});
it("does not verify a recovery receipt that differs from the retained submitted intent", async () => {
  const posted: ReturnType<typeof receiptFor>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/recovery"))
        return new Response(JSON.stringify({ ...posted[0], reason: "TEST different note intent" }));
      if (options?.method === "PUT") {
        posted.push(receiptFor(url, options));
        throw new TypeError("TEST lost response");
      }
      return new Response(
        JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const onRecorded = vi.fn();
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord onRecorded={onRecorded} />);
  await enterCredit(user);
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  await user.click(await screen.findByRole("button", { name: "Check original credit" }));
  expect(await screen.findByText(/original credit could not be verified/)).toBeInTheDocument();
  expect(onRecorded).not.toHaveBeenCalled();
  expect(sessionStorage.length).toBe(1);
  expect(screen.queryByRole("button", { name: "Save supplier credit" })).not.toBeInTheDocument();
});
it("hides malformed foreign source choices and never offers a financial submission", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          data: [{ ...source, outlet_id: source.id }],
          page: { has_more: false, next_cursor: null },
        }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord />);
  await user.click(await screen.findByRole("button", { name: "Record supplier credit" }));
  expect(
    await screen.findByText(/Dispatched financial sources are unavailable/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save supplier credit" })).not.toBeInTheDocument();
});
it("does not submit when a recovery reference cannot be stored", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord />);
  await enterCredit(user);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("TEST storage unavailable");
  });
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(await screen.findByText(/No credit has been submitted/)).toBeInTheDocument();
  expect(fetcher.mock.calls).toHaveLength(1);
});

it("stops new entry clearly when recording authority or release changes before submission", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  function Availability() {
    const [enabled, setEnabled] = useState(true);
    return (
      <>
        <button onClick={() => setEnabled(false)}>Disable recording</button>
        <SupplierCreditRecord scope={scope} canRecord={enabled} />
      </>
    );
  }
  const { user } = render(<Availability />);
  await user.click(await screen.findByRole("button", { name: "Record supplier credit" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched supplier return" }));
  await user.click(await screen.findByRole("option", { name: /Return 17.*TEST Supplier/ }));
  await user.click(screen.getByRole("button", { name: "Disable recording" }));
  expect(
    await screen.findByText(
      "New supplier credit recording is unavailable for this account or release.",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Check original credit" })).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Supplier credit reference")).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
});

it("focuses an invalid actual amount and blocks submission using inline validation", async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () =>
      new Response(
        JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord />);
  await enterCredit(user);
  const amount = screen.getByLabelText("Actual credit amount (INR)");
  await user.clear(amount);
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(
    await screen.findByText("Enter a valid amount within the supported limit."),
  ).toBeInTheDocument();
  await waitFor(() => expect(amount).toHaveFocus());
  expect(fetcher.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);
});

it("retries only the original command and intent after a missing uncertain result", async () => {
  const posted: ReturnType<typeof receiptFor>[] = [];
  const onRecorded = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/recovery"))
        return new Response(JSON.stringify({ error: "not_found", message: "TEST missing" }), {
          status: 404,
        });
      if (options?.method === "PUT") {
        const receipt = receiptFor(url, options);
        posted.push(receipt);
        if (posted.length === 1) throw new TypeError("TEST lost response");
        return new Response(JSON.stringify(receipt));
      }
      return new Response(
        JSON.stringify({ data: [source], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const { user } = render(<SupplierCreditRecord scope={scope} canRecord onRecorded={onRecorded} />);
  await enterCredit(user);
  await user.click(screen.getByRole("button", { name: "Save supplier credit" }));
  expect(await screen.findByText(/credit outcome is unconfirmed/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry exact credit" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Check original credit" }));
  await user.click(await screen.findByRole("button", { name: "Retry exact credit" }));
  await waitFor(() => expect(onRecorded).toHaveBeenCalledWith(posted[0]?.id));
  expect(posted).toHaveLength(2);
  expect(posted[1]).toEqual(posted[0]);
  expect(sessionStorage.length).toBe(0);
});

it("does not expose another account's retained recovery or create a replacement command", async () => {
  const command = "0199e000-0000-7000-8000-000000000099";
  const key = `helivanta:supplier-credit-document:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId])}`;
  sessionStorage.setItem(key, command);
  const fetcher = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <>
      <SupplierCreditRecord
        scope={{ ...scope, subject: "TEST-another-account" }}
        canRecord={false}
      />
      <SupplierCreditRecord scope={scope} canRecord={false} />
    </>,
  );
  expect(await screen.findAllByRole("button", { name: "Check original credit" })).toHaveLength(1);
  expect(fetcher).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Record supplier credit" })).not.toBeInTheDocument();
  expect(sessionStorage.getItem(key)).toBe(command);
  expect(document.body).not.toHaveTextContent(command);
  await user.click(screen.getByRole("button", { name: "Close credit form" }));
});

const voidedCredit = {
  id: "0199e000-0000-7000-8000-000000000008",
  outlet_id: scope.outletId,
  return_id: source.id,
  return_number: source.number,
  payee_reference: "TEST-SUPPLIER",
  payee_name: source.payee_name,
  original_bill_reference: source.original_bill_reference,
  document_reference: "TEST-INCORRECT-NOTE",
  issued_on: "2026-11-18",
  amount_minor: 750,
  currency: "INR" as const,
  reason: "TEST wrong original amount",
  correction_of: null,
  recorded_by: scope.subject,
  recorded_at: "2026-11-19T00:00:00Z",
  version: 2,
  state: "voided" as const,
  matched_minor: 0,
  allocated_minor: 0,
  available_minor: 0,
  local_date: "2026-11-22",
  timezone: "Asia/Kolkata",
  as_of: "2026-11-22T00:00:00Z",
};
it("records an explicit replacement linked to the named voided note and original return without raw identifier entry", async () => {
  const writes: Record<string, unknown>[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method !== "PUT")
      throw new Error(
        "TEST correction must use its verified original, not selectable supplier names",
      );
    const row = receiptFor(url, options);
    writes.push(row);
    return new Response(JSON.stringify(row));
  });
  vi.stubGlobal("fetch", fetcher);
  const completed = vi.fn();
  const { user } = render(
    <SupplierCreditRecord
      scope={scope}
      canRecord
      correction={voidedCredit}
      onRecorded={completed}
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Replace voided credit" }));
  expect(screen.getByText(/TEST-INCORRECT-NOTE/)).toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "Dispatched supplier return" }),
  ).not.toBeInTheDocument();
  expect(screen.getByLabelText("Actual credit amount (INR)")).toHaveValue("");
  await user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CORRECTED-NOTE");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-20" } });
  await user.type(screen.getByLabelText("Actual credit amount (INR)"), "8.00");
  await user.type(
    screen.getByLabelText("Credit record reason"),
    "TEST corrected actual supplier amount",
  );
  await user.click(screen.getByRole("button", { name: "Save replacement credit" }));
  await waitFor(() => expect(completed).toHaveBeenCalledWith(writes[0]?.id));
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    correction_of: voidedCredit.id,
    return_id: source.id,
    document_reference: "TEST-CORRECTED-NOTE",
    amount_minor: 800,
    currency: "INR",
  });
  for (const value of [voidedCredit.id, source.id, scope.subject])
    expect(document.body).not.toHaveTextContent(value);
});

it("recovers the same replacement after reload when the original is corrected and new writes are disabled", async () => {
  const posted: ReturnType<typeof receiptFor>[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      posted.push(receiptFor(url, options));
      throw new TypeError("TEST committed replacement response lost");
    }
    if (url.endsWith("/recovery")) return new Response(JSON.stringify(posted[0]));
    throw new Error("TEST no replacement source selection is required");
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(<SupplierCreditRecord scope={scope} canRecord correction={voidedCredit} />);
  await first.user.click(await screen.findByRole("button", { name: "Replace voided credit" }));
  await first.user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CORRECTED-NOTE");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-20" } });
  await first.user.type(screen.getByLabelText("Actual credit amount (INR)"), "8.00");
  await first.user.type(
    screen.getByLabelText("Credit record reason"),
    "TEST corrected supplier amount",
  );
  await first.user.click(screen.getByRole("button", { name: "Save replacement credit" }));
  expect(await screen.findByText(/credit outcome is unconfirmed/)).toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
  expect(sessionStorage.getItem(sessionStorage.key(0)!)).toBe(posted[0]?.id);
  expect(sessionStorage.key(0)).toContain(voidedCredit.id);
  first.unmount();
  const completed = vi.fn();
  const second = render(
    <SupplierCreditRecord
      scope={scope}
      canRecord={false}
      correction={{ ...voidedCredit, state: "corrected", version: 3 }}
      onRecorded={completed}
    />,
  );
  await second.user.click(await screen.findByRole("button", { name: "Check original credit" }));
  await waitFor(() => expect(completed).toHaveBeenCalledWith(posted[0]?.id));
  expect(posted).toHaveLength(1);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher.mock.calls.some(([url]) => url.includes("source-returns"))).toBe(false);
});
it("refuses a recovered replacement for a different original even without retained form intent", async () => {
  const entry = "0199e000-0000-7000-8000-000000000009";
  sessionStorage.setItem(
    `helivanta:supplier-credit-document:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, voidedCredit.id])}`,
    entry,
  );
  const receipt = receiptFor(`/supplier-credits/${entry}`, {
    body: JSON.stringify({
      return_id: source.id,
      document_reference: "TEST-REPLACEMENT",
      issued_on: "2026-11-20",
      amount_minor: 800,
      currency: "INR",
      reason: "TEST replacement for different original",
      correction_of: "0199e000-0000-7000-8000-000000000007",
    }),
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(receipt))),
  );
  const completed = vi.fn();
  const { user } = render(
    <SupplierCreditRecord
      scope={scope}
      canRecord={false}
      correction={voidedCredit}
      onRecorded={completed}
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Check original credit" }));
  expect(await screen.findByText(/original credit could not be verified/)).toBeInTheDocument();
  expect(completed).not.toHaveBeenCalled();
  expect(sessionStorage.length).toBe(1);
});
it.each(["active", "corrected"] as const)(
  "does not offer a new replacement for an original that is %s",
  (state) => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    render(
      <SupplierCreditRecord scope={scope} canRecord correction={{ ...voidedCredit, state }} />,
    );
    expect(screen.queryByRole("button", { name: "Replace voided credit" })).not.toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
  },
);

it("keeps replacement recovery available after closing when new writes are withdrawn", async () => {
  let posted: ReturnType<typeof receiptFor> | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        posted = receiptFor(url, options);
        throw new TypeError("TEST committed response lost");
      }
      return new Response(JSON.stringify(posted));
    }),
  );
  function Availability() {
    const [enabled, setEnabled] = useState(true);
    return (
      <>
        <button onClick={() => setEnabled(false)}>Withdraw replacement writes</button>
        <SupplierCreditRecord
          scope={scope}
          canRecord={enabled}
          correction={{ ...voidedCredit, state: enabled ? "voided" : "corrected" }}
        />
      </>
    );
  }
  const { user } = render(<Availability />);
  await user.click(await screen.findByRole("button", { name: "Replace voided credit" }));
  await user.type(screen.getByLabelText("Supplier credit reference"), "TEST-CORRECTED-NOTE");
  fireEvent.change(screen.getByLabelText("Credit issue date"), { target: { value: "2026-11-20" } });
  await user.type(screen.getByLabelText("Actual credit amount (INR)"), "8.00");
  await user.type(screen.getByLabelText("Credit record reason"), "TEST corrected supplier amount");
  await user.click(screen.getByRole("button", { name: "Save replacement credit" }));
  expect(await screen.findByText(/credit outcome is unconfirmed/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close credit form" }));
  await user.click(await screen.findByRole("button", { name: "Discard" }));
  await user.click(screen.getByRole("button", { name: "Withdraw replacement writes" }));
  await user.click(await screen.findByRole("button", { name: "Resume supplier credit recovery" }));
  await user.click(await screen.findByRole("button", { name: "Check original credit" }));
  expect(
    await screen.findByText(/Supplier credit TEST-CORRECTED-NOTE verified/),
  ).toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
});
