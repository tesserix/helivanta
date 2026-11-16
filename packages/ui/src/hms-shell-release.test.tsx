import { useEffect } from "react";
import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { releaseSnapshotForTest, renderWithProviders } from "@helivanta/api/testing";
import { HmsShell } from "./hms-shell";

const pathname = vi.hoisted(() => ({ current: "/workforce" }));
vi.mock("next/navigation", () => ({
  usePathname: () => pathname.current,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
afterEach(() => {
  cancelTenantTransition();
  localStorage.clear();
  vi.unstubAllGlobals();
});
function mount(keys: string[], options: { unavailable?: boolean; expired?: boolean } = {}) {
  const mounted = vi.fn();
  function BusinessPage() {
    useEffect(() => {
      mounted();
    }, []);
    return <div>Protected business content</div>;
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["workforce.staff.read"],
        });
      if (url.endsWith("/workspace/release-capabilities")) {
        if (options.unavailable) return Response.json({}, { status: 503 });
        const value = releaseSnapshotForTest(scope, keys);
        if (options.expired) value.expires_at = value.issued_at;
        return Response.json(value);
      }
      return Response.json({});
    }),
  );
  renderWithProviders(
    <HmsShell>
      <BusinessPage />
    </HmsShell>,
  );
  return mounted;
}
it.each([{}, { unavailable: true }, { expired: true }])(
  "does not mount a deep-linked page before valid admission: %j",
  async (options) => {
    pathname.current = "/workforce";
    const mounted = mount([], options);
    expect(
      await screen.findByRole("heading", { name: "This feature is not available" }),
    ).toBeVisible();
    expect(screen.queryByText("Protected business content")).not.toBeInTheDocument();
    expect(mounted).not.toHaveBeenCalled();
  },
);
it("mounts released work and immediately closes it while switching accounts", async () => {
  pathname.current = "/workforce";
  const mounted = mount(["team_accounts", "staff_core"]);
  expect(await screen.findByText("Protected business content")).toBeVisible();
  expect(mounted).toHaveBeenCalledTimes(1);
  act(() => beginTenantTransition());
  await waitFor(() =>
    expect(screen.queryByText("Protected business content")).not.toBeInTheDocument(),
  );
});
it("keeps tenant security reachable when release evaluation is unavailable", async () => {
  pathname.current = "/settings/security";
  const mounted = mount([], { unavailable: true });
  await waitFor(() => expect(screen.getByText("Protected business content")).toBeVisible());
  expect(mounted).toHaveBeenCalledTimes(1);
});
