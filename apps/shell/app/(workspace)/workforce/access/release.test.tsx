import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders, releaseSnapshotForTest } from "@helivanta/api/testing";
import { staffAccessTemplates, staffAccessTemplateCatalogs } from "@helivanta/api";
import { NavigationGuardProvider } from "@helivanta/ui";
import AccessPage from "./page";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "foundation-owner" };
const foundation = [
  "onboarding",
  "team_accounts",
  "staff_core",
  "department_teams",
  "pharmacy_setup",
  "pharmacy_staff",
];
afterEach(() => vi.unstubAllGlobals());
it("an owner sees core assignment choices without loading leave or supplier work", async () => {
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      paths.push(path);
      if (path.endsWith("/workspace/release-capabilities"))
        return Response.json(releaseSnapshotForTest(scope, foundation));
      if (path.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.assignment.manage", "iam.assignment.review"],
        });
      if (path.includes("/staff-access-templates")) {
        const name = new URL(path, "https://synthetic.invalid").searchParams.get(
          "catalog",
        ) as keyof typeof staffAccessTemplateCatalogs;
        return Response.json(
          staffAccessTemplates.filter((row) =>
            (staffAccessTemplateCatalogs[name] as readonly string[])?.includes(row.key),
          ),
        );
      }
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "Assignment area" })).toHaveTextContent(
      "Staff identity checks",
    ),
  );
  await user.click(screen.getByRole("combobox", { name: "Assignment area" }));
  expect(screen.queryByRole("option", { name: "Leave review" })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: "Supplier collaboration" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("option", { name: "Partner team memberships" }),
  ).not.toBeInTheDocument();
  expect(
    paths.some((path) =>
      /hospital-supplier-access|partner-membership|staff-department-leave-reviews|family=leave|family=supplier_access/.test(
        path,
      ),
    ),
  ).toBe(false);
});
