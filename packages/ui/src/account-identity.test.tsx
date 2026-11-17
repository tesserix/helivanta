import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { useState } from "react";
import { useDisplayedAccountPresentations } from "@helivanta/api";
import {
  AccountName,
  AccountRole,
  AccountPresentationsProvider,
  AccountSelectControl,
} from "./account-identity";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };

it("selects accounts by authorized names while passing only the internal binding to the command", async () => {
  const selected = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { subjects } = JSON.parse(init?.body as string);
      return Response.json({
        tenant_id: scope.tenantId,
        actor_subject: scope.subject,
        data: subjects.map((subject: string) => ({
          subject,
          available: true,
          name: subject === "member-a" ? "Alice Staff" : "Bob Staff",
          email: "staff@example.test",
          roles: [{ key: "staff_member", label: "Staff member" }],
        })),
      });
    }),
  );
  const { user } = renderWithProviders(
    <AccountPresentationsProvider scope={scope}>
      <AccountSelectControl
        aria-label="Staff account"
        subjects={["member-a", "member-b"]}
        value="member-a"
        onValueChange={selected}
      />
    </AccountPresentationsProvider>,
  );
  expect(await screen.findByText(/Alice Staff/)).toBeInTheDocument();
  await user.click(screen.getByRole("combobox", { name: "Staff account" }));
  await user.click(await screen.findByRole("option", { name: /Bob Staff/ }));
  expect(selected).toHaveBeenCalledWith("member-b");
  expect(screen.queryByText("member-a")).not.toBeInTheDocument();
  expect(screen.queryByText("member-b")).not.toBeInTheDocument();
});
afterEach(() => vi.unstubAllGlobals());

it("keeps current member names readable when a historical actor no longer has membership", async () => {
  const fetcher = vi.fn(async (_url: string, init?: RequestInit) => {
    const { subjects } = JSON.parse(init?.body as string);
    return Response.json({
      tenant_id: scope.tenantId,
      actor_subject: scope.subject,
      data: subjects
        .filter((subject: string) => subject !== "removed-member")
        .map((subject: string) => ({
          subject,
          available: true,
          name: "Current Staff",
          email: "staff@example.test",
          roles: [{ key: "staff_member", label: "Staff member" }],
        })),
    });
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <AccountPresentationsProvider scope={scope}>
      <AccountName subject="current-member" />
      <AccountName subject="removed-member" />
    </AccountPresentationsProvider>,
  );
  expect(await screen.findByText("Current Staff")).toBeInTheDocument();
  expect(screen.getByText("Account details unavailable")).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(screen.queryByText("removed-member")).not.toBeInTheDocument();
});

it("batches displayed accounts and shows authorized names and role labels without identifiers", async () => {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    expect(url).toBe("/api/v1/iam/account-presentations");
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    const body = JSON.parse(init?.body as string);
    expect(body.subjects).toEqual(["synthetic-member"]);
    return Response.json({
      tenant_id: scope.tenantId,
      actor_subject: scope.subject,
      data: [
        {
          subject: "synthetic-member",
          available: true,
          name: "QA Staff",
          email: "staff@example.test",
          roles: [{ key: "staff_member", label: "Staff member" }],
        },
      ],
    });
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <AccountPresentationsProvider scope={scope}>
      <AccountName subject="synthetic-member" />
      <AccountRole subject="synthetic-member" roleKey="staff_member" />
    </AccountPresentationsProvider>,
  );
  expect(await screen.findByText("QA Staff")).toBeInTheDocument();
  expect(screen.getByText("Staff member")).toBeInTheDocument();
  expect(screen.queryByText("synthetic-member")).not.toBeInTheDocument();
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
});

it("withdraws cached names when their current authorization read fails", async () => {
  let refused = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      refused
        ? Response.json({ error: "forbidden" }, { status: 403 })
        : Response.json({
            tenant_id: scope.tenantId,
            actor_subject: scope.subject,
            data: [
              {
                subject: "synthetic-member",
                available: true,
                name: "Private Staff name",
                email: "staff@example.test",
                roles: [{ key: "staff_member", label: "Staff member" }],
              },
            ],
          }),
    ),
  );
  function Recheck() {
    const query = useDisplayedAccountPresentations(scope, ["synthetic-member"]);
    return (
      <button
        onClick={() => {
          void query.refetch();
        }}
      >
        Recheck names
      </button>
    );
  }
  const { user } = renderWithProviders(
    <AccountPresentationsProvider scope={scope}>
      <AccountName subject="synthetic-member" />
      <Recheck />
    </AccountPresentationsProvider>,
  );
  await screen.findByText("Private Staff name");
  refused = true;
  await user.click(screen.getByRole("button", { name: "Recheck names" }));
  await screen.findByText("Account details unavailable");
  expect(screen.queryByText("Private Staff name")).not.toBeInTheDocument();
  expect(screen.queryByText("synthetic-member")).not.toBeInTheDocument();
});

it("removes the prior actor's names immediately during account switching", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const actor = new Headers(init?.headers).get("X-Helivanta-Expected-Subject");
      return Response.json({
        tenant_id: scope.tenantId,
        actor_subject: actor,
        data: [
          {
            subject: "synthetic-member",
            available: actor === scope.subject,
            ...(actor === scope.subject
              ? { name: "First account's private name", email: "staff@example.test" }
              : {}),
            roles: [{ key: "staff_member", label: "Staff member" }],
          },
        ],
      });
    }),
  );
  function Switch() {
    const [actor, setActor] = useState(scope.subject);
    return (
      <>
        <button onClick={() => setActor("synthetic-other-actor")}>Change account</button>
        <AccountPresentationsProvider scope={{ ...scope, subject: actor }}>
          <AccountName subject="synthetic-member" />
        </AccountPresentationsProvider>
      </>
    );
  }
  const { user } = renderWithProviders(<Switch />);
  await screen.findByText("First account's private name");
  await user.click(screen.getByRole("button", { name: "Change account" }));
  expect(screen.queryByText("First account's private name")).not.toBeInTheDocument();
  expect(await screen.findByText("Account details unavailable")).toBeInTheDocument();
});

it("keeps nullable actor copy and tooltip names readable without rendering identifiers", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        tenant_id: scope.tenantId,
        actor_subject: scope.subject,
        data: [
          {
            subject: "synthetic-member",
            available: true,
            name: "QA Reviewer",
            email: "reviewer@example.test",
            roles: [{ key: "staff_member", label: "Staff member" }],
          },
        ],
      }),
    ),
  );
  renderWithProviders(
    <AccountPresentationsProvider scope={scope}>
      <AccountName subject="synthetic-member">
        {(name) => <span title={`Reviewed by ${name}`}>Reviewed</span>}
      </AccountName>
      <AccountName subject={null}>{(name) => <span>{name ?? "No decision yet"}</span>}</AccountName>
    </AccountPresentationsProvider>,
  );
  expect(await screen.findByTitle("Reviewed by QA Reviewer")).toBeInTheDocument();
  expect(screen.getByText("No decision yet")).toBeInTheDocument();
  expect(screen.queryByText("synthetic-member")).not.toBeInTheDocument();
});
