import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider, useConfirmLeave } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import { StockPanel } from "./stock-panel";

// A live address: the chooser is the URL (#1303 E4).
const nav = vi.hoisted(() => ({
  search: "",
  listeners: new Set<() => void>(),
  calls: [] as { method: "push" | "replace"; href: string }[],
}));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) => {
    nav.calls.push({ method, href });
    nav.search = href.split("?")[1] ?? "";
    nav.listeners.forEach((fn) => fn());
  };
  return {
    usePathname: () => "/pharmacy/receiving",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
afterEach(() => {
  vi.unstubAllGlobals();
  nav.search = "";
  nav.calls.length = 0;
});

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const location = "0199e000-0000-7000-8000-000000000004";
const other = "0199e000-0000-7000-8000-00000000000b";
const product = "0199e000-0000-7000-8000-000000000003";
const batch = "0199e000-0000-7000-8000-000000000005";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });

function mockReceiving() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/stock/locations"))
        return new Response(
          JSON.stringify(
            page([
              { id: location, code: "TEST-Q", name: "TEST Quarantine" },
              { id: other, code: "TEST-Q2", name: "TEST Second" },
            ]),
          ),
        );
      if (url.includes("/catalogue/products"))
        return new Response(
          JSON.stringify(page([{ id: product, sku: "TEST-500", brand_name: "TEST capsule" }])),
        );
      if (url.includes("/stock/batches"))
        return new Response(
          JSON.stringify(
            page([
              { id: batch, product_id: product, lot_number: "TEST-LOT", expires_on: "2030-10-31" },
            ]),
          ),
        );
      if (url.includes("/stock/balance"))
        return new Response(
          JSON.stringify({ on_hand_units: 7, dispensable_units: 0, stock_status: "quarantine" }),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
}
// Stands in for a shell link: it asks the shell's guard before it moves.
const leftTo = vi.fn();
function ShellLink() {
  const leave = useConfirmLeave();
  return (
    <button type="button" onClick={() => leave(() => leftTo("/dashboard"))}>
      Dashboard
    </button>
  );
}
const renderReceiving = () =>
  renderWithProviders(
    <NavigationGuardProvider>
      <ShellLink />
      <StockPanel scope={scope} writable correctable />
    </NavigationGuardProvider>,
  );

describe("Receiving shows the chosen lot beside the chooser (#1303 E4)", () => {
  it("keeps the chosen location, product and lot through a reload", async () => {
    nav.search = `location=${location}&product=${product}&open=${batch}`;
    mockReceiving();
    renderReceiving();
    const pane = await screen.findByRole("region", { name: "Lot TEST-LOT at TEST-Q" });
    expect(pane).toHaveTextContent("Selected lot balance");
    expect(await screen.findByText("7")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next lot" })).toBeNull();
  });

  it("opens the lot in the pane once location, product and lot are chosen", async () => {
    mockReceiving();
    const { user } = renderReceiving();
    await chooseOption(user, await screen.findByLabelText("Quarantine location"), /^TEST-Q ·/);
    await chooseOption(user, screen.getByLabelText("Product"), "TEST-500 · TEST capsule");
    await chooseOption(user, screen.getByLabelText("Manufacturer lot"), /^TEST-LOT/);
    expect(await screen.findByRole("region", { name: "Lot TEST-LOT at TEST-Q" })).toBeVisible();
    const address = new URLSearchParams(nav.search);
    expect(Object.fromEntries(address)).toEqual({ location, product, open: batch });
    // Opening is a Back step; the chooser before it replaced.
    expect(nav.calls.at(-1)?.method).toBe("push");
  });

  it("asks before another location leaves an unsaved movement", async () => {
    nav.search = `location=${location}&product=${product}&open=${batch}`;
    mockReceiving();
    const { user } = renderReceiving();
    await screen.findByRole("region", { name: "Lot TEST-LOT at TEST-Q" });
    await user.type(
      await screen.findByLabelText("Receipt / correction line reference"),
      "TEST-GRN-1",
    );
    await chooseOption(user, screen.getByLabelText("Quarantine location"), /^TEST-Q2 ·/);
    expect(await screen.findByRole("dialog")).toHaveTextContent("Discard changes to this lot?");
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(new URLSearchParams(nav.search).get("location")).toBe(location);
    expect(screen.getByLabelText("Receipt / correction line reference")).toHaveValue("TEST-GRN-1");
  });

  it("asks before a shell link leaves an unsaved new location", async () => {
    mockReceiving();
    const { user } = renderReceiving();
    await user.type(await screen.findByLabelText("Location code"), "TEST-NEW");
    await user.click(screen.getByRole("button", { name: "Dashboard" }));
    expect(await screen.findByRole("dialog")).toBeVisible();
    expect(leftTo).not.toHaveBeenCalled();
  });
});
