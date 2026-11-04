import { useState, type ReactElement } from "react";
import { act, fireEvent, screen, within, waitFor } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  StaffPostingCutoffInputContract,
  type StaffPostingCutoff,
  type StaffPostingCutoffInput,
  type StaffPostingProposal,
} from "@helivanta/api";
import { chooseOption } from "@/test/select";
import { StaffPostingCutoffWorkspace } from "./posting-cutoff-workspace";
import { PostingCutoffPage } from "./posting-cutoff-page";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-cutoff-administrator",
};
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const approved: StaffPostingProposal = {
  id: id(1),
  revision_id: id(2),
  command_id: id(3),
  hospital_id: hospital,
  staff_id: staff,
  version: 2,
  status: "approved",
  grants_access: false,
  draft_id: id(4),
  draft_revision_id: id(5),
  draft_version: 1,
  generation_id: id(6),
  staff_version: 2,
  employment_epoch: 2,
  department_id: id(7),
  department_revision_id: id(8),
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
  eligibility_until: "2027-01-01T00:00:00Z",
  created_at: "2026-11-01T00:00:00Z",
  recorded_at: "2026-11-01T00:00:01Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function setup() {
  const state = {
    writes: [] as Array<{ url: string; body: StaffPostingCutoffInput }>,
    receipt: null as StaffPostingCutoff | null,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: scope.tenantId, subject: scope.subject, data: [] });
      if (url.includes("posting-cutoff-scopes"))
        return Response.json(page([{ hospital_id: hospital, name: "Synthetic hospital" }]));
      if (url.includes("posting-proposal-inbox"))
        return Response.json(
          page([
            {
              proposal: approved,
              staff: {
                id: staff,
                employee_code: "SYN-CUTOFF",
                display_name: "Synthetic canonical Staff",
              },
              is_submitter: false,
            },
          ]),
        );
      if (url.includes("/cutoffs/context"))
        return Response.json({
          proposal: approved,
          cutoff: state.receipt,
          checked_at: new Date().toISOString(),
        });
      if (url.includes("/cutoffs/history"))
        return Response.json(page(state.receipt ? [state.receipt] : []));
      if (init?.method === "PUT") {
        const body = StaffPostingCutoffInputContract.parse(JSON.parse(String(init.body)));
        state.writes.push({ url, body });
        const recorded = new Date().toISOString();
        state.receipt = {
          ...body,
          id: url.split("/").at(-1)!,
          hospital_id: hospital,
          staff_id: staff,
          proposal_id: approved.id,
          version: body.expected_cutoff_version + 1,
          requested_cutoff_at: body.cutoff_at ?? null,
          cutoff_at: body.cutoff_at ?? recorded,
          recorded_at: recorded,
        };
        return Response.json(state.receipt);
      }
      if (url.includes("/cutoffs/commands/"))
        return state.receipt && url.endsWith(state.receipt.id)
          ? Response.json(state.receipt)
          : Response.json({ error: "not_found" }, { status: 404 });
      throw new Error("Unexpected synthetic cutoff UI route: " + url);
    }),
  );
  return state;
}

async function openWorkspace(
  ui: ReactElement = <StaffPostingCutoffWorkspace scope={scope} onContextChanged={() => {}} />,
) {
  const view = renderWithProviders(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
  const { user } = view;
  await user.click(
    await screen.findByRole("button", { name: "Open posting cutoffs for Synthetic hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Manage cutoff for Synthetic canonical Staff" }),
  );
  const form = await screen.findByRole("form", { name: "Posting cutoff" });
  return { ...view, form };
}

it("discovers canonical postings and requires rationale and examination before ending one", async () => {
  const { writes } = setup();
  const { user, form } = await openWorkspace();
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  expect(writes).toHaveLength(0);
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic end of departmental responsibility",
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  expect(writes).toHaveLength(0);
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(writes).toHaveLength(1);
  expect(writes[0].url).toContain(
    `/posting-staff/${staff}/posting-proposals/${approved.id}/cutoffs/commands/`,
  );
  expect(writes[0].body).toEqual({
    expected_revision_id: approved.revision_id,
    expected_cutoff_version: 0,
    mode: "immediate",
    reason: "Synthetic end of departmental responsibility",
    sources_examined: true,
  });
});

it("recovers a committed cutoff after a lost response without issuing another write", async () => {
  const state = setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const result = await prior(input, init);
      if (init?.method === "PUT") throw new TypeError("Synthetic lost committed cutoff response");
      return result;
    }),
  );
  const { user, form } = await openWorkspace();
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic retained ending after a lost response",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByRole("button", { name: "Recover cutoff receipt" });
  expect(within(form).getByLabelText("Cutoff rationale")).toHaveValue(
    "Synthetic retained ending after a lost response",
  );
  expect(within(form).getByLabelText("Cutoff rationale")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover cutoff receipt" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(state.writes).toHaveLength(1);
  const recovery = vi
    .mocked(fetch)
    .mock.calls.filter(
      ([url, init]) => String(url).includes("/cutoffs/commands/") && init?.method !== "PUT",
    );
  expect(recovery).toHaveLength(1);
  expect(String(recovery[0][0])).toBe(state.writes[0].url);
});

it("retains an unknown command after receipt404 and retries the identical cutoff", async () => {
  const state = setup();
  const prior = fetch;
  let first = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PUT" && first) {
        first = false;
        state.writes.push({
          url: String(input),
          body: StaffPostingCutoffInputContract.parse(JSON.parse(String(init.body))),
        });
        throw new TypeError("Synthetic interrupted cutoff before persistence");
      }
      return prior(input, init);
    }),
  );
  const { user, form } = await openWorkspace();
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic identical cutoff retry",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await user.click(await screen.findByRole("button", { name: "Recover cutoff receipt" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Retry exact cutoff" })).toBeEnabled(),
  );
  expect(within(form).getByLabelText("Cutoff rationale")).toHaveValue(
    "Synthetic identical cutoff retry",
  );
  await user.click(screen.getByRole("button", { name: "Retry exact cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(state.writes).toHaveLength(2);
  expect(state.writes[1]).toEqual(state.writes[0]);
});

it("requires an explicit future UTC instant before scheduling a cutoff", async () => {
  const { writes } = setup();
  const { user, form } = await openWorkspace();
  await chooseOption(
    user,
    within(form).getByLabelText("Ending mode"),
    "Schedule an earlier cutoff",
  );
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic scheduled responsibility ending",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  expect(writes).toHaveLength(0);
  const past = new Date(Date.now() - 60_000).toISOString().slice(0, 16);
  fireEvent.change(within(form).getByLabelText("Scheduled cutoff (UTC)"), {
    target: { value: past },
  });
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Choose a future cutoff instant.");
  expect(writes).toHaveLength(0);
  const future = new Date(Date.now() + 86_400_000).toISOString().slice(0, 16);
  fireEvent.change(within(form).getByLabelText("Scheduled cutoff (UTC)"), {
    target: { value: future },
  });
  await screen.findByText(new RegExp("Selected instant: " + future));
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(writes).toHaveLength(1);
  expect(writes[0].body.cutoff_at).toBe(future + ":00Z");
  expect(writes[0].body.mode).toBe("scheduled");
});

it("blocks extending a retained cutoff and pins the version when shortening it", async () => {
  const state = setup();
  const currentAt = new Date(Date.now() + 86_400_000).toISOString();
  state.receipt = {
    id: id(20),
    hospital_id: hospital,
    staff_id: staff,
    proposal_id: approved.id,
    expected_revision_id: approved.revision_id,
    expected_cutoff_version: 0,
    version: 1,
    mode: "scheduled",
    requested_cutoff_at: currentAt,
    cutoff_at: currentAt,
    recorded_at: new Date(Date.now() - 60_000).toISOString(),
    reason: "Synthetic existing responsibility cutoff",
    sources_examined: true,
  };
  const { user, form } = await openWorkspace();
  await chooseOption(
    user,
    within(form).getByLabelText("Ending mode"),
    "Schedule an earlier cutoff",
  );
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic shortening of retained responsibility",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  fireEvent.change(within(form).getByLabelText("Scheduled cutoff (UTC)"), {
    target: { value: new Date(Date.now() + 2 * 86_400_000).toISOString().slice(0, 16) },
  });
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Choose a cutoff earlier than the retained cutoff.");
  expect(state.writes).toHaveLength(0);
  const earlier = new Date(Date.now() + 43_200_000).toISOString().slice(0, 16);
  fireEvent.change(within(form).getByLabelText("Scheduled cutoff (UTC)"), {
    target: { value: earlier },
  });
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(state.writes[0].body.expected_cutoff_version).toBe(1);
  expect(state.writes[0].body.cutoff_at).toBe(earlier + ":00Z");
});

it("clears the protected workspace when cutoff authority is revoked", async () => {
  setup();
  const prior = fetch;
  let revoked = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (revoked && String(input).includes("/cutoffs/context"))
        return Response.json({ error: "forbidden" }, { status: 403 });
      return prior(input, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PostingCutoffPage />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting cutoffs for Synthetic hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Manage cutoff for Synthetic canonical Staff" }),
  );
  await screen.findByRole("form", { name: "Posting cutoff" });
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Recheck posting cutoff" }));
  await screen.findByRole("button", { name: "Reopen posting cutoffs" });
  expect(screen.queryByText("Synthetic canonical Staff")).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Posting cutoff" })).not.toBeInTheDocument();
});

it("drops selected Staff and entered rationale when the account scope changes", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (
        String(input).includes("posting-cutoff-scopes") &&
        new Headers(init?.headers).get("X-Helivanta-Expected-Subject") ===
          "synthetic-other-cutoff-actor"
      )
        return Response.json(page([]));
      return prior(input, init);
    }),
  );
  let switchAccount: (next: typeof scope) => void = () => {};
  function AccountSwitcher() {
    const [account, setAccount] = useState(scope);
    switchAccount = setAccount;
    return <StaffPostingCutoffWorkspace scope={account} onContextChanged={() => {}} />;
  }
  const { user, form } = await openWorkspace(<AccountSwitcher />);
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic private retained rationale",
  );
  act(() => switchAccount({ ...scope, subject: "synthetic-other-cutoff-actor" }));
  await screen.findByText("No approved posting cutoff hospitals on this page.");
  expect(screen.queryByText("Synthetic canonical Staff")).not.toBeInTheDocument();
  expect(
    screen.queryByDisplayValue("Synthetic private retained rationale"),
  ).not.toBeInTheDocument();
});

it("requires renewed examination when a refreshed cutoff changes during preparation", async () => {
  const state = setup();
  const { user, form } = await openWorkspace();
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic rationale retained across a peer cutoff",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  const instant = new Date(Date.now() + 86_400_000).toISOString();
  state.receipt = {
    id: id(30),
    hospital_id: hospital,
    staff_id: staff,
    proposal_id: approved.id,
    expected_revision_id: approved.revision_id,
    expected_cutoff_version: 0,
    version: 1,
    mode: "scheduled",
    requested_cutoff_at: instant,
    cutoff_at: instant,
    recorded_at: new Date(Date.now() - 1000).toISOString(),
    reason: "Synthetic peer shortened the responsibility",
    sources_examined: true,
  };
  await user.click(screen.getByRole("button", { name: "Recheck posting cutoff" }));
  await screen.findByText("Synthetic peer shortened the responsibility");
  expect(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  ).not.toBeChecked();
  expect(within(form).getByLabelText("Cutoff rationale")).toHaveValue(
    "Synthetic rationale retained across a peer cutoff",
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  expect(state.writes).toHaveLength(0);
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  expect(state.writes[0].body.expected_cutoff_version).toBe(1);
});

it("keeps recovery available but blocks an unrecorded retry after the observed cutoff elapses", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  const baseline = Date.parse("2026-11-05T00:00:00Z");
  vi.setSystemTime(baseline);
  try {
    const state = setup();
    const instant = new Date(baseline + 60_000).toISOString();
    state.receipt = {
      id: id(40),
      hospital_id: hospital,
      staff_id: staff,
      proposal_id: approved.id,
      expected_revision_id: approved.revision_id,
      expected_cutoff_version: 0,
      version: 1,
      mode: "scheduled",
      requested_cutoff_at: instant,
      cutoff_at: instant,
      recorded_at: new Date(baseline - 1000).toISOString(),
      reason: "Synthetic retained cutoff before its effective instant",
      sources_examined: true,
    };
    const prior = fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "PUT") {
          state.writes.push({
            url: String(input),
            body: StaffPostingCutoffInputContract.parse(JSON.parse(String(init.body))),
          });
          throw new TypeError("Synthetic interrupted ending before persistence");
        }
        return prior(input, init);
      }),
    );
    const { user, form } = await openWorkspace();
    await user.type(
      within(form).getByLabelText("Cutoff rationale"),
      "Synthetic retained intent across cutoff expiry",
    );
    await user.click(
      within(form).getByLabelText(
        "I examined the approved posting and its current cutoff history.",
      ),
    );
    await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
    await screen.findByRole("button", { name: "Recover cutoff receipt" });
    act(() => vi.setSystemTime(baseline + 120_000));
    await user.click(screen.getByRole("button", { name: "Recover cutoff receipt" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Recheck posting cutoff" })).toBeEnabled(),
    );
    expect(screen.getByRole("button", { name: "Retry exact cutoff" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Recover cutoff receipt" })).toBeEnabled();
    expect(within(form).getByLabelText("Cutoff rationale")).toHaveValue(
      "Synthetic retained intent across cutoff expiry",
    );
    expect(state.writes).toHaveLength(1);
  } finally {
    vi.useRealTimers();
  }
});

it("loads retained history on demand after a saved cutoff", async () => {
  setup();
  const { user, form } = await openWorkspace();
  expect(
    vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes("/cutoffs/history")),
  ).toBe(false);
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic retained history through the normal workspace",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await screen.findByText("Posting cutoff recorded. Approval history is preserved.");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "View cutoff history" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "View cutoff history" }));
  const history = await screen.findByRole("region", { name: "Cutoff history" });
  expect(
    within(history).getByText("Synthetic retained history through the normal workspace"),
  ).toBeInTheDocument();
  expect(within(history).getByText("Cutoff v1 · Immediate")).toBeInTheDocument();
});

it("withholds the form and never invents an empty cutoff when context is unavailable", async () => {
  const { writes } = setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) =>
      String(input).includes("/cutoffs/context")
        ? Response.json({ error: "posting_source_unavailable" }, { status: 503 })
        : prior(input, init),
    ),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingCutoffWorkspace scope={scope} onContextChanged={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting cutoffs for Synthetic hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Manage cutoff for Synthetic canonical Staff" }),
  );
  await screen.findByText("The posting cutoff context could not be verified.");
  expect(screen.queryByText("No cutoff has been recorded.")).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Posting cutoff" })).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("aborts an in-flight cutoff on account change without replaying it into the new scope", async () => {
  const state = setup();
  const prior = fetch;
  let finish: (value: Response) => void = () => {};
  let signal: AbortSignal | undefined;
  let switchAccount: (next: typeof scope) => void = () => {};
  function AccountSwitcher() {
    const [account, setAccount] = useState(scope);
    switchAccount = setAccount;
    return <StaffPostingCutoffWorkspace scope={account} onContextChanged={() => {}} />;
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (
        String(input).includes("posting-cutoff-scopes") &&
        new Headers(init?.headers).get("X-Helivanta-Expected-Subject") ===
          "synthetic-switched-cutoff-actor"
      )
        return Response.json(page([]));
      if (init?.method === "PUT") {
        state.writes.push({
          url: String(input),
          body: StaffPostingCutoffInputContract.parse(JSON.parse(String(init.body))),
        });
        signal = init.signal ?? undefined;
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      }
      return prior(input, init);
    }),
  );
  const { user, form } = await openWorkspace(<AccountSwitcher />);
  await user.type(
    within(form).getByLabelText("Cutoff rationale"),
    "Synthetic in-flight private cutoff",
  );
  await user.click(
    within(form).getByLabelText("I examined the approved posting and its current cutoff history."),
  );
  await user.click(within(form).getByRole("button", { name: "Record posting cutoff" }));
  await waitFor(() => expect(state.writes).toHaveLength(1));
  act(() => switchAccount({ ...scope, subject: "synthetic-switched-cutoff-actor" }));
  await screen.findByText("No approved posting cutoff hospitals on this page.");
  expect(signal?.aborted).toBe(true);
  const recorded = new Date().toISOString();
  await act(async () => {
    finish(
      Response.json({
        ...state.writes[0].body,
        id: state.writes[0].url.split("/").at(-1),
        hospital_id: hospital,
        staff_id: staff,
        proposal_id: approved.id,
        version: 1,
        requested_cutoff_at: null,
        cutoff_at: recorded,
        recorded_at: recorded,
      }),
    );
  });
  expect(state.writes).toHaveLength(1);
  expect(
    screen.queryByText("Posting cutoff recorded. Approval history is preserved."),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("Synthetic canonical Staff")).not.toBeInTheDocument();
  expect(screen.queryByDisplayValue("Synthetic in-flight private cutoff")).not.toBeInTheDocument();
});
