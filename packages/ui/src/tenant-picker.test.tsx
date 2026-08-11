import { describe, expect, it, vi, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { TenantPicker } from "./tenant-picker";

afterEach(() => vi.unstubAllGlobals());

describe("TenantPicker", () => {
  it("renders nothing for a single-tenant user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [{ tenant_id: "t1", roles: ["doctor"] }] }),
      }),
    );
    renderWithProviders(<TenantPicker />);

    // renderWithProviders always mounts a sonner <Toaster/> alongside the
    // component under test, so the render container is never fully empty —
    // assert on the picker's own markup instead.
    await waitFor(() => expect(screen.queryByText("Hospital")).not.toBeInTheDocument());
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("lists every tenant for a multi-hospital user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          data: [
            { tenant_id: "t1", roles: ["doctor"] },
            { tenant_id: "t2", roles: ["nurse"] },
          ],
        }),
      }),
    );
    renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());
    expect(screen.getAllByRole("option")).toHaveLength(2);
  });
});
