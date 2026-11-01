import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AdmissionDiscounts, DiscountReasonsSetup, PendingDiscounts } from "./discounts";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const reason = {
  id: "0199e000-0000-7000-8000-0000000000c1",
  hospital_id: hospital.id,
  code: "staff",
  name: "Staff dependant",
  desk_percent_bp: 1000,
  desk_max_minor: 500000,
  approval_percent_bp: 5000,
  approval_max_minor: 5000000,
  currency: "INR",
  created_by: "user-admin",
  created_at: "2026-11-01T08:00:00Z",
  retired_by: null,
  retired_at: null,
};
const retired = {
  ...reason,
  id: "0199e000-0000-7000-8000-0000000000c2",
  code: "camp",
  name: "Health camp",
  retired_by: "user-admin",
  retired_at: "2026-11-01T09:00:00Z",
};
const pending = {
  id: "0199e000-0000-7000-8000-0000000000d1",
  admission_id: admission,
  patient_id: "0199e000-0000-7000-8000-0000000000p1",
  reason_id: reason.id,
  reason_code: "staff",
  reason_name: "Staff dependant",
  amount_minor: 800000,
  currency: "INR",
  justification: "Nurse's mother",
  status: "pending",
  requested_by: "user-clerk",
  requested_at: "2026-11-01T10:00:00Z",
  decided_by: null,
  decided_at: null,
  decision_note: null,
  applied_at: null,
  net_minor: 0,
};
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });

afterEach(() => vi.unstubAllGlobals());

function server(permissions: string[], answer: unknown = {}) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-clerk", data: permissions });
    if (init.method === "PUT" || init.method === "POST") return json(201, answer);
    if (url.includes("/discount-reasons")) return page([reason, retired]);
    if (url.includes("/pending-discounts")) return page([pending]);
    if (url.includes("/discounts")) return page([pending]);
    if (url.includes("/patients/")) return json(404, { error: "not_found" });
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const writes = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "PUT" || init?.method === "POST") as [
    string,
    RequestInit,
  ][];

describe("AdmissionDiscounts", () => {
  it("asks for a discount on an active reason and says what the server decided", async () => {
    const fetcher = server(["billing.bill.issue"], { ...pending, amount_minor: 100000 });
    const { user } = renderWithProviders(
      <AdmissionDiscounts hospital={hospital} admissionId={admission} discountable={300000} />,
    );
    await user.click(await screen.findByRole("combobox", { name: "Reason" }));
    expect(screen.queryByRole("option", { name: "Health camp" })).toBeNull();
    await user.click(screen.getByRole("option", { name: "Staff dependant" }));
    await user.clear(screen.getByLabelText("Discount (INR)"));
    await user.type(screen.getByLabelText("Discount (INR)"), "1000");
    await user.type(screen.getByLabelText("Why"), "Nurse's mother");
    await user.click(screen.getByRole("button", { name: "Give discount" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    const [url, init] = writes(fetcher)[0];
    expect(url).toMatch(new RegExp(`/admissions/${admission}/discounts/[0-9a-f-]{36}$`));
    expect(JSON.parse(String(init.body))).toEqual({
      reason_id: reason.id,
      amount_minor: 100000,
      justification: "Nurse's mother",
    });
    expect(await screen.findByRole("status")).toHaveTextContent(
      /Discount of .*1,000\.00 sent for approval/,
    );
  });

  it("refuses more than the charges can take before asking", async () => {
    const fetcher = server(["billing.bill.issue"]);
    const { user } = renderWithProviders(
      <AdmissionDiscounts hospital={hospital} admissionId={admission} discountable={300000} />,
    );
    await user.click(await screen.findByRole("combobox", { name: "Reason" }));
    await user.click(screen.getByRole("option", { name: "Staff dependant" }));
    await user.clear(screen.getByLabelText("Discount (INR)"));
    await user.type(screen.getByLabelText("Discount (INR)"), "3001");
    await user.type(screen.getByLabelText("Why"), "Too much");
    await user.click(screen.getByRole("button", { name: "Give discount" }));
    expect(await screen.findByText(/^At most .*3,000\.00$/)).toBeInTheDocument();
    expect(writes(fetcher)).toHaveLength(0);
  });

  it("offers no form when nothing can be discounted, or without the permission", async () => {
    server(["billing.bill.issue"]);
    const { unmount } = renderWithProviders(
      <AdmissionDiscounts hospital={hospital} admissionId={admission} discountable={0} />,
    );
    expect(await screen.findByText(/Nurse's mother/)).toBeInTheDocument();
    expect(screen.queryByRole("form", { name: "Give a discount" })).toBeNull();
    unmount();
    server(["billing.account.read"]);
    renderWithProviders(
      <AdmissionDiscounts hospital={hospital} admissionId={admission} discountable={300000} />,
    );
    expect(await screen.findByText(/Nurse's mother/)).toBeInTheDocument();
    expect(screen.queryByRole("form", { name: "Give a discount" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });
});

describe("PendingDiscounts", () => {
  it("rejects with a note, and approves with one", async () => {
    const fetcher = server(["billing.discount.approve"], { ...pending, status: "rejected" });
    const { user } = renderWithProviders(<PendingDiscounts hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Reject" }));
    expect(await screen.findByText("Add a note")).toBeInTheDocument();
    expect(writes(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("Note"), "No staff card");
    await user.click(screen.getByRole("button", { name: "Reject" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(writes(fetcher)[0][0]).toMatch(new RegExp(`/discounts/${pending.id}/decision$`));
    expect(JSON.parse(String(writes(fetcher)[0][1].body))).toEqual({
      approve: false,
      note: "No staff card",
    });
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(2));
    expect(JSON.parse(String(writes(fetcher)[1][1].body))).toEqual({
      approve: true,
      note: "No staff card",
    });
  });

  it("is not shown without the approval permission", async () => {
    const fetcher = server(["billing.bill.issue"]);
    renderWithProviders(<PendingDiscounts hospital={hospital} />);
    await waitFor(() =>
      expect(fetcher.mock.calls.some(([u]) => u.includes("/iam/me/permissions"))).toBe(true),
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText("Discounts waiting for approval")).toBeNull();
    expect(fetcher.mock.calls.some(([u]) => u.includes("/pending-discounts"))).toBe(false);
  });
});

describe("DiscountReasonsSetup", () => {
  it("adds a reason in basis points and paise, and retires an active one", async () => {
    const fetcher = server(["billing.discount.manage"], reason);
    const { user } = renderWithProviders(<DiscountReasonsSetup hospital={hospital} />);
    expect(await screen.findByText(/Health camp/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retire Health camp" })).toBeNull();
    await user.type(screen.getByLabelText("Code"), "goodwill");
    await user.type(screen.getByLabelText("Name"), "Service recovery");
    await user.type(screen.getByLabelText("Desk %"), "12.5");
    await user.clear(screen.getByLabelText("Desk up to (INR)"));
    await user.type(screen.getByLabelText("Desk up to (INR)"), "500");
    await user.type(screen.getByLabelText("Approval %"), "10");
    await user.clear(screen.getByLabelText("Approval up to (INR)"));
    await user.type(screen.getByLabelText("Approval up to (INR)"), "5000");
    await user.click(screen.getByRole("button", { name: "Add reason" }));
    expect(await screen.findByText("Not above the approval percentage")).toBeInTheDocument();
    expect(writes(fetcher)).toHaveLength(0);
    await user.clear(screen.getByLabelText("Approval %"));
    await user.type(screen.getByLabelText("Approval %"), "20");
    await user.click(screen.getByRole("button", { name: "Add reason" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(writes(fetcher)[0][1].body))).toEqual({
      code: "goodwill",
      name: "Service recovery",
      desk_percent_bp: 1250,
      desk_max_minor: 50000,
      approval_percent_bp: 2000,
      approval_max_minor: 500000,
    });
    await user.click(screen.getByRole("button", { name: "Retire Staff dependant" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(2));
    expect(writes(fetcher)[1][0]).toMatch(new RegExp(`/discount-reasons/${reason.id}/retire$`));
  });
});
