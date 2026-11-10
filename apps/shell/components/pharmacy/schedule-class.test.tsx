import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { formatZonedDateTime, type PharmacyProduct } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { ProductScheduleSection } from "./schedule-class";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};

const product = {
  id: "0199e000-0000-7000-8000-000000000101",
  sku: "TEST-AMOX-500",
  brand_name: "TEST Amoxil",
  status: "active",
} as PharmacyProduct;

const declaration = {
  id: "0199e000-0000-7000-8000-000000000701",
  product_id: product.id,
  regulatory_class: "H1",
  ndps: false,
  source: "TEST pack label",
  declared_by: "user-declarer",
  created_at: "2026-11-09T04:00:00Z",
  decision: null,
  decision_reason: null,
  reviewed_by: null,
  reviewed_at: null,
};
const approved = {
  ...declaration,
  decision: "approved",
  decision_reason: "TEST pack checked",
  reviewed_by: "user-reviewer",
  reviewed_at: "2026-11-09T05:00:00Z",
};
const inForce = (row: typeof approved) => ({
  in_force: row,
  origin: "declared_by_outlet",
  reason: "",
  history: [row],
});
const awaiting = {
  in_force: null,
  origin: "declared_by_outlet",
  reason: "the latest declaration awaits review by a second person",
  history: [declaration],
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type Answer = { status?: number; body: unknown };
function serve(state: unknown | Answer[], writes: Answer[] = []) {
  const reads = Array.isArray(state) ? [...state] : null;
  const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async (_url, init) => {
      const answer =
        init?.method === "PUT" || init?.method === "POST"
          ? (writes.shift() ?? { status: 201, body: declaration })
          : reads
            ? reads.length > 1
              ? reads.shift()!
              : reads[0]!
            : { body: state };
      return new Response(JSON.stringify(answer.body), { status: answer.status ?? 200 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const writes = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls.filter(([, init]) => init?.method === "PUT" || init?.method === "POST");
const reads = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls.filter(([url, init]) => !init?.method && url.endsWith("/schedule"));

function renderSection(
  props: Partial<Parameters<typeof ProductScheduleSection>[0]> = {},
): ReturnType<typeof renderWithProviders> {
  return renderWithProviders(
    <ProductScheduleSection
      scope={scope}
      product={product}
      timeZone="Asia/Kolkata"
      canDeclare={false}
      canReview={false}
      {...props}
    />,
  );
}

describe("ProductScheduleSection", () => {
  it("shows the class in force with its approval time", async () => {
    serve(inForce(approved));
    renderSection();
    expect(
      await screen.findByText(
        `Schedule H1 · not NDPS, approved ${formatZonedDateTime(approved.reviewed_at, "Asia/Kolkata")}`,
      ),
    ).toBeVisible();
    const history = screen.getByRole("list", { name: "Schedule class history" });
    expect(history).toHaveTextContent("TEST pack label");
    expect(history).toHaveTextContent("user-declarer");
    expect(history).toHaveTextContent("Approved by user-reviewer: TEST pack checked");
  });

  it("reads class none as not scheduled", async () => {
    serve(inForce({ ...approved, regulatory_class: "none" }));
    renderSection();
    expect(await screen.findByText(/^Not scheduled · not NDPS, approved/)).toBeVisible();
  });

  it("says none is in force, with the API's reason", async () => {
    serve(awaiting);
    renderSection();
    expect(
      await screen.findByText(
        "No schedule class in force: the latest declaration awaits review by a second person",
      ),
    ).toBeVisible();
    expect(screen.getByText(/cannot be released or sold/)).toBeVisible();
  });

  it.each([
    ["Schedule X", { ...approved, regulatory_class: "X" }],
    ["NDPS", { ...approved, ndps: true }],
  ])("warns that %s cannot be released or sold yet", async (_, row) => {
    serve(inForce(row));
    renderSection();
    expect(
      await screen.findByText(/Schedule X and NDPS products cannot be released or sold/),
    ).toBeVisible();
  });

  it("says only the latest 50 declarations are shown", async () => {
    const history = Array.from({ length: 50 }, (_, i) => ({
      ...approved,
      id: `0199e000-0000-7000-8000-${String(i).padStart(12, "0")}`,
    }));
    serve({ ...inForce(approved), history });
    renderSection();
    expect(await screen.findByText("Showing the latest 50 declarations.")).toBeVisible();
  });

  it("reports a failed read through RequestFailure, and asks again", async () => {
    const fetcher = serve([
      { status: 503, body: { error: "unavailable", message: "no" } },
      { body: inForce(approved) },
    ]);
    const { user } = renderSection();
    expect(await screen.findByText("Could not load the schedule class.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText(/^Schedule H1 · not NDPS, approved/)).toBeVisible();
    expect(reads(fetcher)).toHaveLength(2);
  });

  it("ends the session on a 401 with no retry", async () => {
    serve([{ status: 401, body: { error: "unauthenticated", message: "no" } }]);
    renderSection();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your session has ended. Sign in again to continue.",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });

  it("offers no forms without the grants", async () => {
    serve(awaiting);
    renderSection();
    await screen.findByText(/No schedule class in force/);
    expect(screen.queryByRole("button", { name: "Declare schedule class" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });
});

describe("declaring a schedule class", () => {
  const none = {
    in_force: null,
    origin: "declared_by_outlet",
    reason: "no schedule class has been declared",
    history: [],
  };
  const declare = () => screen.getByRole("button", { name: "Declare schedule class" });

  it("requires the class and NDPS to be chosen, with no default", async () => {
    const fetcher = serve(none);
    const { user } = renderSection({ canDeclare: true });
    await user.type(await screen.findByLabelText("Source"), "pack label");
    await user.click(declare());
    expect(await screen.findByText("Choose the schedule class")).toBeVisible();
    expect(screen.getByText("Say whether it is under NDPS")).toBeVisible();
    expect(writes(fetcher)).toHaveLength(0);
  });

  it("refuses a blank source", async () => {
    const fetcher = serve(none);
    const { user } = renderSection({ canDeclare: true });
    await chooseOption(user, await screen.findByRole("combobox", { name: "Class" }), "H");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), "   ");
    await user.click(declare());
    expect(await screen.findByText("Name the source, such as the pack label")).toBeVisible();
    expect(writes(fetcher)).toHaveLength(0);
  });

  it("declares with the chosen values", async () => {
    const fetcher = serve(none);
    const { user } = renderSection({ canDeclare: true });
    await chooseOption(user, await screen.findByRole("combobox", { name: "Class" }), "H");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), " pack label ");
    await user.click(declare());
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    const [url, init] = writes(fetcher)[0]!;
    expect(init?.method).toBe("PUT");
    expect(url).toMatch(
      /\/catalogue\/products\/[0-9a-f-]{36}\/schedule\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$/,
    );
    expect(JSON.parse(String(init?.body))).toEqual({
      regulatory_class: "H",
      ndps: false,
      source: "pack label",
    });
    expect(await screen.findByText("Schedule class declared")).toBeVisible();
    await waitFor(() => expect(reads(fetcher)).toHaveLength(2));
  });

  it("records NDPS yes as true and class none as none", async () => {
    const fetcher = serve(none);
    const { user } = renderSection({ canDeclare: true });
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Class" }),
      "Not scheduled",
    );
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "Yes");
    await user.type(screen.getByLabelText("Source"), "pack label");
    await user.click(declare());
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(writes(fetcher)[0]![1]?.body))).toMatchObject({
      regulatory_class: "none",
      ndps: true,
    });
  });

  it("reuses the id when a failed declaration is retried", async () => {
    const fetcher = serve(none, [
      {
        status: 503,
        body: { error: "unavailable", message: "The pharmacy service is unavailable." },
      },
      { status: 201, body: declaration },
    ]);
    const { user } = renderSection({ canDeclare: true });
    await chooseOption(user, await screen.findByRole("combobox", { name: "Class" }), "H1");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), "pack label");
    await user.click(declare());
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The pharmacy service is unavailable.",
    );
    await user.click(declare());
    await waitFor(() => expect(writes(fetcher)).toHaveLength(2));
    expect(writes(fetcher)[1]![0]).toBe(writes(fetcher)[0]![0]);
  });

  // A PUT that committed but answered 503 must not leave a form whose id is spent.
  it("rereads after a failed declaration, so a committed one shows and the form takes a new id", async () => {
    const fetcher = serve(
      [{ body: none }, { body: awaiting }],
      [
        {
          status: 503,
          body: { error: "unavailable", message: "The pharmacy service is unavailable." },
        },
      ],
    );
    const { user } = renderSection({ canDeclare: true });
    await chooseOption(user, await screen.findByRole("combobox", { name: "Class" }), "H1");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), "pack label");
    await user.click(declare());
    expect(await screen.findByText("Awaiting review")).toBeVisible();
    expect(reads(fetcher)).toHaveLength(2);
    await chooseOption(user, screen.getByRole("combobox", { name: "Class" }), "G");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), "second label");
    await user.click(declare());
    await waitFor(() => expect(writes(fetcher)).toHaveLength(2));
    expect(writes(fetcher)[1]![0]).not.toBe(writes(fetcher)[0]![0]);
  });

  it("offers no declaration on a withdrawn product", async () => {
    serve(none);
    renderSection({ canDeclare: true, product: { ...product, status: "withdrawn" } });
    await screen.findByText(/No schedule class in force/);
    expect(screen.queryByRole("button", { name: "Declare schedule class" })).toBeNull();
  });
});

describe("reviewing a schedule class", () => {
  const older = {
    ...declaration,
    id: "0199e000-0000-7000-8000-000000000700",
    regulatory_class: "H",
  };
  const reviewOf = (fetcher: ReturnType<typeof serve>) =>
    writes(fetcher).filter(([, i]) => i?.method === "POST");

  it("approves the newest pending declaration with what was checked", async () => {
    const fetcher = serve(awaiting, [{ status: 200, body: approved }]);
    const { user } = renderSection({ canReview: true });
    expect(await screen.findByText("Awaiting review")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Approve" }));
    expect(await screen.findByText("Say what you checked.")).toBeVisible();
    expect(writes(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("What you checked"), "TEST pack checked");
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(reviewOf(fetcher)).toHaveLength(1));
    const [url, init] = reviewOf(fetcher)[0]!;
    expect(url).toMatch(new RegExp(`/schedule/${declaration.id}/review$`));
    expect(JSON.parse(String(init?.body))).toEqual({
      decision: "approved",
      reason: "TEST pack checked",
    });
    expect(await screen.findByText("Review recorded")).toBeVisible();
    await waitFor(() => expect(reads(fetcher)).toHaveLength(2));
  });

  it("rejects with a reason", async () => {
    const fetcher = serve(awaiting, [
      { status: 200, body: { ...declaration, decision: "rejected" } },
    ]);
    const { user } = renderSection({ canReview: true });
    await user.type(await screen.findByLabelText("What you checked"), "wrong pack");
    await user.click(screen.getByRole("button", { name: "Reject" }));
    await waitFor(() => expect(reviewOf(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(reviewOf(fetcher)[0]![1]?.body))).toEqual({
      decision: "rejected",
      reason: "wrong pack",
    });
  });

  it("marks an older pending declaration superseded and offers no review of it", async () => {
    serve({ ...awaiting, history: [declaration, older] });
    renderSection({ canReview: true });
    const history = await screen.findByRole("list", { name: "Schedule class history" });
    const [newest, previous] = within(history).getAllByRole("listitem");
    expect(newest).toHaveTextContent("Awaiting review");
    expect(previous).toHaveTextContent("Superseded by a newer declaration");
    expect(screen.getAllByRole("button", { name: "Approve" })).toHaveLength(1);
    expect(screen.getByText(/Awaiting review: Schedule H1 · not NDPS/)).toBeVisible();
  });

  it("offers no review once the newest declaration is decided, even with an older one pending", async () => {
    serve({ ...inForce(approved), history: [approved, older] });
    renderSection({ canReview: true });
    await screen.findByRole("list", { name: "Schedule class history" });
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });

  it("shows the four-eyes refusal inline", async () => {
    const message = "a schedule class is reviewed by someone other than who declared it";
    const fetcher = serve(awaiting, [{ status: 409, body: { error: "four_eyes", message } }]);
    const { user } = renderSection({ canReview: true });
    await user.type(await screen.findByLabelText("What you checked"), "my own");
    await user.click(screen.getByRole("button", { name: "Approve" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(reads(fetcher)).toHaveLength(1);
  });

  it.each([
    [409, { error: "conflict", message: "this declaration has already been reviewed" }],
    [404, { error: "not_found", message: "schedule declaration not found" }],
  ])("rereads the schedule class when a review answers %s", async (status, body) => {
    const fetcher = serve([{ body: awaiting }, { body: inForce(approved) }], [{ status, body }]);
    const { user } = renderSection({ canReview: true });
    await user.type(await screen.findByLabelText("What you checked"), "late");
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(reads(fetcher)).toHaveLength(2));
    expect(await screen.findByText(/^Schedule H1 · not NDPS, approved/)).toBeVisible();
  });

  it("ties a refused review reason to its field", async () => {
    serve(awaiting);
    const { user } = renderSection({ canReview: true });
    await user.click(await screen.findByRole("button", { name: "Approve" }));
    const field = screen.getByLabelText("What you checked");
    expect(await screen.findByText("Say what you checked.")).toBeVisible();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription("Say what you checked.");
  });

  // The review and declaration forms are keyed on the same declaration id; as
  // siblings that rendered the review form twice once a declaration landed.
  it("renders one review form for someone who may both declare and review", async () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    // As in the browser journey: an approved class, then a new declaration lands.
    const before = {
      ...approved,
      id: "0199e000-0000-7000-8000-000000000699",
      regulatory_class: "none",
    };
    serve([{ body: inForce(before) }, { body: { ...awaiting, history: [declaration, before] } }]);
    const { user } = renderSection({ canDeclare: true, canReview: true });
    await chooseOption(user, await screen.findByRole("combobox", { name: "Class" }), "H1");
    await chooseOption(user, screen.getByRole("combobox", { name: "NDPS" }), "No");
    await user.type(screen.getByLabelText("Source"), "pack label");
    await user.click(screen.getByRole("button", { name: "Declare schedule class" }));
    expect(await screen.findByText("Awaiting review")).toBeVisible();
    expect(screen.getAllByRole("button", { name: "Approve" })).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: "Declare schedule class" })).toHaveLength(1);
    await user.type(screen.getByLabelText("What you checked"), "TEST checked");
    expect(screen.getAllByRole("button", { name: "Approve" })).toHaveLength(1);
    expect(errors.mock.calls.flat().join(" ")).not.toContain("same key");
  });

  it("allows a review on a withdrawn product", async () => {
    serve(awaiting);
    renderSection({ canReview: true, product: { ...product, status: "withdrawn" } });
    expect(await screen.findByRole("button", { name: "Approve" })).toBeVisible();
  });
});
