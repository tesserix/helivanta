import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  beginTenantTransition,
  cancelTenantTransition,
  capabilityForPage,
  usePermissions,
  useReleaseCapabilities,
} from "@helivanta/api";
import {
  renderWithProviders,
  releaseSnapshotForTest,
  releaseDefinitionsForTest,
} from "@helivanta/api/testing";
import { ZONES } from "@helivanta/ui";
import Dashboard from "./page";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-release-owner",
};
const mvp = [
  "onboarding",
  "team_accounts",
  "staff_core",
  "department_teams",
  "pharmacy_setup",
  "pharmacy_staff",
];
const mvpCards = [
  "/pharmacy/staff",
  "/pharmacy/setup",
  "/workforce",
  "/workforce/pharmacy-staff",
  "/workforce/department-teams",
  "/workforce/postings",
  "/workforce/posting-reviews",
  "/workforce/pharmacy-relationships",
  "/workforce/employment-reviews",
  "/workforce/person-reviews",
  "/workforce/access",
  "/workforce/setup",
  "/workforce/credentials",
  "/settings/security",
  "/settings/organization",
  "/settings/branding",
].sort();
const permissions = [
  ...new Set(
    ZONES.flatMap((zone) => [
      zone.permission,
      ...zone.pages.flatMap((page) => [page.permission, ...(page.alternativePermissions ?? [])]),
    ]),
  ),
];

function effectiveWithout(key: string) {
  const enabled = new Set(mvp.filter((item) => item !== key));
  for (const definition of releaseDefinitionsForTest)
    if (definition.dependencies.some((dependency) => !enabled.has(dependency)))
      enabled.delete(definition.key);
  return [...enabled];
}

function mockOwner(read: () => Response) {
  vi.stubGlobal("fetch", async (url: string) => {
    if (url.endsWith("/iam/me/permissions"))
      return Response.json({
        tenant_id: scope.tenantId,
        subject: scope.subject,
        data: permissions,
      });
    if (url.endsWith("/workspace/release-capabilities")) return read();
    throw new Error(`Unexpected dashboard business request: ${url}`);
  });
}

function cardPaths() {
  const navigation = screen.getByRole("navigation", { name: "Department shortcuts" });
  return within(navigation)
    .getAllByRole("link")
    .map((link) => link.getAttribute("href"))
    .sort();
}

function RefreshCapabilities() {
  const release = useReleaseCapabilities();
  const identity = usePermissions();
  return (
    <>
      <button onClick={() => void release.refresh()}>Refresh test availability</button>
      <p role="status">
        {!identity.subject || identity.isLoading || release.isLoading
          ? "Test availability pending"
          : release.error
            ? "Test availability refused"
            : "Test availability settled"}
      </p>
    </>
  );
}

function mountReleaseDashboard() {
  return renderWithProviders(
    <>
      <Dashboard />
      <RefreshCapabilities />
    </>,
  );
}

afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

it("shows only enabled MVP cards when recovery is eligible and the owner has every permission", async () => {
  expect(
    releaseDefinitionsForTest.find((item) => item.key === "staff_mfa_recovery")?.eligible,
  ).toBe(true);
  mockOwner(() => Response.json(releaseSnapshotForTest(scope, mvp)));
  mountReleaseDashboard();
  await screen.findByText("Test availability settled");
  await waitFor(() => expect(cardPaths()).toEqual(mvpCards));
  expect(screen.queryByText("Hospital overview")).not.toBeInTheDocument();
});

it.each(mvp)("removes every unavailable destination when %s is disabled", async (disabled) => {
  const enabled = effectiveWithout(disabled);
  mockOwner(() => Response.json(releaseSnapshotForTest(scope, enabled)));
  mountReleaseDashboard();
  await screen.findByText("Test availability settled");
  const expected = mvpCards.filter((path) => {
    const key = capabilityForPage(path);
    return key === "baseline" || enabled.includes(key ?? "");
  });
  await waitFor(() => expect(cardPaths()).toEqual(expected));
});

it("updates existing cards when a refreshed effective flag changes", async () => {
  let enabled = mvp;
  mockOwner(() => Response.json(releaseSnapshotForTest(scope, enabled)));
  const { user } = mountReleaseDashboard();
  await waitFor(() => expect(cardPaths()).toEqual(mvpCards));
  enabled = effectiveWithout("team_accounts");
  await user.click(screen.getByRole("button", { name: "Refresh test availability" }));
  await waitFor(() => expect(cardPaths()).toEqual(["/settings/security"]));
  enabled = mvp;
  await user.click(screen.getByRole("button", { name: "Refresh test availability" }));
  await waitFor(() => expect(cardPaths()).toEqual(mvpCards));
});

it.each(["unavailable", "wrong account"])(
  "withholds business cards for %s release state",
  async (state) => {
    mockOwner(() =>
      state === "unavailable"
        ? Response.json({ error: "unavailable" }, { status: 503 })
        : Response.json(releaseSnapshotForTest({ ...scope, subject: "another-account" }, mvp)),
    );
    mountReleaseDashboard();
    await screen.findByText("Test availability refused");
    await waitFor(() => expect(cardPaths()).toEqual(["/settings/security"]));
  },
);

it("immediately removes the previous tenant's cards during account switching", async () => {
  mockOwner(() => Response.json(releaseSnapshotForTest(scope, mvp)));
  mountReleaseDashboard();
  await waitFor(() => expect(cardPaths()).toEqual(mvpCards));
  act(() => beginTenantTransition());
  expect(cardPaths()).toEqual(["/settings/security"]);
});
