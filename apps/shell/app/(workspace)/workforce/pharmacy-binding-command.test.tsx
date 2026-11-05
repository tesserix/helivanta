import { chooseOption } from "@/test/select";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { screen, waitFor } from "@testing-library/react";
import { PharmacyBindingCommand } from "./pharmacy-binding-command";

const id = (n: number) => "019a0000-0000-7000-8000-" + String(n).padStart(12, "0");
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  pharmacyId: id(1),
  outletId: id(2),
};
const department = {
  id: id(4),
  revision_id: id(5),
  version: 1,
  hospital_id: scope.hospitalId,
  code: "PHARMACY",
  name: "Synthetic Pharmacy Department",
  kind: "department",
  status: "active",
  parent_id: null,
  effective_at: "2026-11-05T00:00:00Z",
  created_at: "2026-11-05T00:00:00Z",
};
const authority = (action: string) =>
  ["pharmacy.staff_binding." + action, "workforce.staff_actor.use"].map((permission) => ({
    permission,
    assignment_id: id(8),
    assignment_version: 1,
    scope_epoch: 1,
    membership_epoch: 1,
  }));
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const calls: { url: string; options: RequestInit; receipt: unknown }[] = [];
function prepareReceipt(url: string, options: RequestInit) {
  const input = JSON.parse(options.body as string);
  const commandId = url.split("/").at(-1)!;
  const at = "2026-11-05T01:00:00Z";
  const binding = {
    ...input,
    id: commandId,
    pharmacy_id: scope.pharmacyId,
    outlet_id: scope.outletId,
    hospital_id: scope.hospitalId,
    status: "pending",
    version: 1,
    proposed_by_subject: scope.subject,
    proposer_staff_id: id(6),
    created_at: at,
    decided_by_subject: null,
    reviewer_staff_id: null,
    decision_reason: null,
    decided_at: null,
    ended_at: null,
  };
  return {
    id: commandId,
    binding_id: commandId,
    operation: "propose",
    expected_version: 0,
    reason: input.reason,
    replaced_binding_id: null,
    replaced_binding_version: null,
    actor_subject: scope.subject,
    actor_staff_id: id(6),
    recorded_at: at,
    binding,
    authorizations: authority("prepare"),
  };
}
afterEach(() => {
  vi.unstubAllGlobals();
  calls.length = 0;
});
it("prepares a canonical department revision and shows its retained proposal receipt", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) => {
      if (url.includes("posting-department-choices")) return Response.json(page([department]));
      if (options.method === "PUT") {
        const receipt = prepareReceipt(url, options);
        calls.push({ url, options, receipt });
        return Response.json(receipt);
      }
      throw new Error("Unexpected request: " + url);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic responsibility review");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  expect(await screen.findByText("Retained proposal receipt verified")).toBeInTheDocument();
  expect(calls).toHaveLength(1);
  expect(JSON.parse(calls[0]!.options.body as string)).toEqual({
    department_id: department.id,
    department_revision_id: department.revision_id,
    replaces_binding_id: null,
    replaces_binding_version: null,
    reason: "Synthetic responsibility review",
  });
});

it("retains the exact uncertain proposal through a missing receipt and explicit retry", async () => {
  let puts = 0;
  const fetch = vi.fn(async (url: string, options: RequestInit) => {
    if (url.includes("posting-department-choices")) return Response.json(page([department]));
    if (options.method === "PUT") {
      const receipt = prepareReceipt(url, options);
      calls.push({ url, options, receipt });
      if (++puts === 1) throw new TypeError("Synthetic lost response");
      return Response.json(receipt);
    }
    if (url.includes("/commands/"))
      return Response.json({ error: "not_found", message: "Unavailable" }, { status: 404 });
    throw new Error("Unexpected request: " + url);
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic retained intent");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  await screen.findByText(/The outcome could not be confirmed/);
  expect(screen.getByLabelText("Proposal reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover command receipt" }));
  await screen.findByText(
    "No retained receipt is visible yet. The original request may still be in flight.",
  );
  expect(puts).toBe(1);
  expect(fetch.mock.calls.at(-1)![0]).toBe(
    calls[0]!.url + "/commands/" + calls[0]!.url.split("/").at(-1),
  );
  await user.click(screen.getByRole("button", { name: "Retry exact command" }));
  await screen.findByText("Retained proposal receipt verified");
  expect(calls[1]!.url).toBe(calls[0]!.url);
  expect(calls[1]!.options.body).toBe(calls[0]!.options.body);
});

const pending = {
  id: id(3),
  pharmacy_id: scope.pharmacyId,
  outlet_id: scope.outletId,
  hospital_id: scope.hospitalId,
  department_id: department.id,
  department_revision_id: department.revision_id,
  replaces_binding_id: null,
  replaces_binding_version: null,
  reason: "Synthetic recorded proposal",
  status: "pending" as const,
  version: 1,
  proposed_by_subject: scope.subject,
  proposer_staff_id: id(6),
  created_at: "2026-11-05T00:00:00Z",
  decided_by_subject: null,
  reviewer_staff_id: null,
  decision_reason: null,
  decided_at: null,
  ended_at: null,
};

it("requires an explicit independent attestation before approving the exact current department revision", async () => {
  const reviewer = { ...scope, subject: "synthetic-reviewer" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) => {
      if (url.includes("posting-department-choices")) return Response.json(page([department]));
      if (options.method === "POST") {
        const input = JSON.parse(options.body as string),
          at = "2026-11-05T01:00:00Z";
        const binding = {
          ...pending,
          status: "approved",
          version: 2,
          decided_by_subject: reviewer.subject,
          reviewer_staff_id: id(7),
          decision_reason: input.reason,
          decided_at: at,
        };
        const receipt = {
          id: input.id,
          binding_id: binding.id,
          operation: "approve",
          expected_version: 1,
          reason: input.reason,
          replaced_binding_id: null,
          replaced_binding_version: null,
          actor_subject: reviewer.subject,
          actor_staff_id: id(7),
          recorded_at: at,
          binding,
          authorizations: authority("review"),
        };
        calls.push({ url, options, receipt });
        return Response.json(receipt);
      }
      throw new Error("Unexpected request: " + url);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={reviewer}
        intent={{ kind: "decision", binding: pending, operation: "approve" }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Current department revision verified");
  await user.type(screen.getByLabelText("Decision reason"), "Synthetic independent review");
  await user.click(screen.getByRole("button", { name: "Approve relationship" }));
  expect(calls).toHaveLength(0);
  expect(
    await screen.findByText("Confirm the independent department responsibility review."),
  ).toBeInTheDocument();
  await user.click(
    screen.getByLabelText(
      "I independently verified this department is responsible for this outlet.",
    ),
  );
  await user.click(screen.getByRole("button", { name: "Approve relationship" }));
  expect(await screen.findByText("Retained approve receipt verified")).toBeInTheDocument();
  expect(calls).toHaveLength(1);
  expect(JSON.parse(calls[0]!.options.body as string)).toMatchObject({
    operation: "approve",
    expected_version: 1,
    reason: "Synthetic independent review",
    attest_department_responsibility: true,
  });
});

it.each(["reject", "withdraw", "end"] as const)(
  "records %s without depending on current department metadata",
  async (operation) => {
    const withdrawing = operation === "withdraw",
      ending = operation === "end";
    const actor = { ...scope, subject: withdrawing ? scope.subject : "synthetic-reviewer" };
    const selected = ending
      ? {
          ...pending,
          status: "approved" as const,
          version: 2,
          decided_by_subject: "earlier-reviewer",
          reviewer_staff_id: id(7),
          decision_reason: "Earlier independent review",
          decided_at: "2026-11-05T00:30:00Z",
        }
      : pending;
    const fetch = vi.fn(async (url: string, options: RequestInit) => {
      if (options.method !== "POST")
        throw new Error("This action must not need a department read: " + url);
      const input = JSON.parse(options.body as string),
        at = "2026-11-05T01:00:00Z";
      const binding = {
        ...selected,
        status: ending ? "ended" : withdrawing ? "withdrawn" : "rejected",
        version: input.expected_version + 1,
        ...(ending
          ? { ended_at: at }
          : {
              decided_by_subject: actor.subject,
              reviewer_staff_id: withdrawing ? id(6) : id(7),
              decision_reason: input.reason,
              decided_at: at,
            }),
      };
      const receipt = {
        id: input.id,
        binding_id: binding.id,
        operation,
        expected_version: input.expected_version,
        reason: input.reason,
        replaced_binding_id: null,
        replaced_binding_version: null,
        actor_subject: actor.subject,
        actor_staff_id: withdrawing ? id(6) : id(7),
        recorded_at: at,
        binding,
        authorizations: authority(withdrawing ? "prepare" : "review"),
      };
      calls.push({ url, options, receipt });
      return Response.json(receipt);
    });
    vi.stubGlobal("fetch", fetch);
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyBindingCommand
          scope={actor}
          intent={{ kind: "decision", binding: selected, operation }}
          onBusy={() => {}}
          onCompleted={() => {}}
          onContextChanged={() => {}}
        />
      </NavigationGuardProvider>,
    );
    await user.type(screen.getByLabelText("Decision reason"), "Synthetic lifecycle reason");
    await user.click(
      screen.getByRole("button", {
        name: ending
          ? "End relationship"
          : withdrawing
            ? "Withdraw proposal"
            : "Reject relationship",
      }),
    );
    await screen.findByText("Retained " + operation + " receipt verified");
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(JSON.parse(calls[0]!.options.body as string)).toMatchObject({
      operation,
      expected_version: ending ? 2 : 1,
      attest_department_responsibility: false,
    });
  },
);

it("does not allow the proposing account to review its own relationship", async () => {
  const fetch = vi.fn(async () => Response.json(page([department])));
  vi.stubGlobal("fetch", fetch);
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "decision", binding: pending, operation: "approve" }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Current department revision verified");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Recheck department source" })).toBeEnabled(),
  );
  expect(screen.getByRole("button", { name: "Approve relationship" })).toBeDisabled();
  expect(
    screen.getByText(
      "An independent reviewer is required. Only the original proposer may withdraw.",
    ),
  ).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("withholds approval when the canonical department revision changed", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([{ ...department, revision_id: id(25), version: 2 }]))),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={{ ...scope, subject: "synthetic-reviewer" }}
        intent={{ kind: "decision", binding: pending, operation: "approve" }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText(
    "The exact current department revision could not yet be verified. Check further pages or recheck its source.",
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Recheck department source" })).toBeEnabled(),
  );
  expect(screen.getByRole("button", { name: "Approve relationship" })).toBeDisabled();
  expect(screen.queryByText("Current department revision verified")).not.toBeInTheDocument();
});

it("pins the exact approved replacement identity and version in a new proposal", async () => {
  const replacement = {
    ...pending,
    id: id(30),
    status: "approved" as const,
    version: 2,
    decided_by_subject: "earlier-reviewer",
    reviewer_staff_id: id(7),
    decision_reason: "Earlier review",
    decided_at: "2026-11-05T00:30:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) => {
      if (url.includes("posting-department-choices")) return Response.json(page([department]));
      const receipt = prepareReceipt(url, options);
      calls.push({ url, options, receipt });
      return Response.json(receipt);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic replacement responsibility");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  await screen.findByText("Retained proposal receipt verified");
  expect(JSON.parse(calls[0]!.options.body as string)).toMatchObject({
    replaces_binding_id: replacement.id,
    replaces_binding_version: 2,
  });
});

it("requires a canonical department and nonblank reason before creating a command", async () => {
  const fetch = vi.fn(async () => Response.json(page([department])));
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByLabelText("Department");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  expect(await screen.findByRole("alert")).toBeInTheDocument();
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic missing department");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  expect(
    await screen.findByText("Choose a department on the verified current page."),
  ).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("requires a reviewer to see the retained replacement and its ending consequence", async () => {
  const replacement = { ...pending, replaces_binding_id: id(30), replaces_binding_version: 2 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([department]))),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={{ ...scope, subject: "synthetic-reviewer" }}
        intent={{ kind: "decision", binding: replacement, operation: "approve" }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Current department revision verified");
  expect(
    screen.getByText(
      "Approval will end the previous relationship " +
        replacement.replaces_binding_id +
        " · Version 2 in the same transaction.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.getByLabelText(
      "I independently verified this department's responsibility and the replacement of the previous relationship.",
    ),
  ).toBeInTheDocument();
});

it("keeps a flat or unverifiable response uncertain instead of claiming a saved proposal", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) => {
      if (url.includes("posting-department-choices")) return Response.json(page([department]));
      const receipt = prepareReceipt(url, options);
      calls.push({ url, options, receipt });
      return Response.json(receipt.binding);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic invalid envelope");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  await screen.findByText(/The outcome could not be confirmed/);
  expect(screen.queryByText("Retained proposal receipt verified")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Recover command receipt" })).toBeEnabled();
  expect(calls).toHaveLength(1);
});

it("holds a pending command against double submission and navigation until its receipt arrives", async () => {
  let release!: (response: Response) => void;
  const response = new Promise<Response>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) => {
      if (url.includes("posting-department-choices")) return Response.json(page([department]));
      const receipt = prepareReceipt(url, options);
      calls.push({ url, options, receipt });
      return response;
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic pending command");
  await user.dblClick(screen.getByRole("button", { name: "Record relationship proposal" }));
  expect(calls).toHaveLength(1);
  expect(screen.getByRole("button", { name: "Recover command receipt" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Retry exact command" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close command editor" })).toBeDisabled();
  release(Response.json(calls[0]!.receipt));
  await screen.findByText("Retained proposal receipt verified");
  expect(calls).toHaveLength(1);
});

it("clears private command fields when current command access is denied", async () => {
  const cutoff = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes("posting-department-choices")
        ? Response.json(page([department]))
        : Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
    ),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyBindingCommand
        scope={scope}
        intent={{ kind: "proposal", replacement: null }}
        onBusy={() => {}}
        onCompleted={() => {}}
        onContextChanged={cutoff}
      />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Department"),
    department.code + " · " + department.name,
  );
  await user.type(screen.getByLabelText("Proposal reason"), "Synthetic protected rationale");
  await user.click(screen.getByRole("button", { name: "Record relationship proposal" }));
  await screen.findByText("Staff access changed. Reopen Staff to verify your current workspace.");
  expect(screen.queryByLabelText("Proposal reason")).not.toBeInTheDocument();
  expect(screen.queryByDisplayValue("Synthetic protected rationale")).not.toBeInTheDocument();
  expect(cutoff).toHaveBeenCalledTimes(1);
});
