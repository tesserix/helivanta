import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { BarcodeInput } from "./barcode-input";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const product = {
  id: "0199e000-0000-7000-8000-000000000003",
  sku: "TEST",
  brand_name: "Test product",
  manufacturer: "Test manufacturer",
  pack_size: 10,
  unit_of_issue: "capsule",
  status: "active",
};
afterEach(() => vi.unstubAllGlobals());

it("rejects an overlong scanner payload without looking up a truncated prefix", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <BarcodeInput scope={scope} canLink={false} onProduct={vi.fn()} />,
  );
  const barcode = screen.getByLabelText("Barcode");
  await user.type(barcode, `${"0".repeat(129)}{Enter}`);
  expect(await screen.findByRole("alert")).toHaveTextContent("at most 128 characters");
  expect(barcode).toHaveValue("0".repeat(129));
  expect(barcode).toHaveAttribute("aria-invalid", "true");
  expect(fetch).not.toHaveBeenCalled();
});

it("accepts keyboard scanner Enter, resolves the outlet product and restores focus", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(product)));
  vi.stubGlobal("fetch", fetch);
  const add = vi.fn().mockResolvedValue("Added one unit");
  const { user } = renderWithProviders(<BarcodeInput scope={scope} canLink onProduct={add} />);
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  await waitFor(() => expect(add).toHaveBeenCalledWith(product, expect.any(AbortSignal)));
  expect(fetch.mock.calls[0][0]).toContain(
    "/outlets/0199e000-0000-7000-8000-000000000001/catalogue/barcodes/4006381333931",
  );
  expect(screen.getByLabelText("Barcode")).toHaveValue("");
  expect(screen.getByLabelText("Barcode")).toHaveFocus();
  expect(screen.getByRole("status")).toHaveTextContent("Added one unit");
});

it("offers an unknown barcode link, saves the chosen product and processes it", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/catalogue/products"))
      return new Response(
        JSON.stringify({ data: [product], page: { has_more: false, next_cursor: null } }),
      );
    if (init?.method === "PUT") return new Response(JSON.stringify(product));
    return new Response(
      JSON.stringify({ error: "barcode_unknown", message: "Barcode not linked" }),
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  const add = vi.fn().mockResolvedValue("Selected Test product");
  const { user } = renderWithProviders(<BarcodeInput scope={scope} canLink onProduct={add} />);
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  await user.click(await screen.findByRole("button", { name: "Link barcode to a product" }));
  await chooseOption(user, await screen.findByLabelText("Link to product"), /TEST/);
  await user.click(screen.getByRole("button", { name: "Save barcode link" }));
  await waitFor(() => expect(add).toHaveBeenCalled());
  expect(fetch.mock.calls.find(([, init]) => init?.method === "PUT")?.[1]?.body).toBe(
    JSON.stringify({ product_id: product.id }),
  );
});

it("keeps failed input for retry and never offers linking for a forbidden lookup", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "forbidden", message: "You do not have access" }), {
        status: 403,
      }),
    ),
  );
  const add = vi.fn();
  const { user } = renderWithProviders(<BarcodeInput scope={scope} canLink onProduct={add} />);
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
  expect(screen.getByLabelText("Barcode")).toHaveValue("4006381333931");
  expect(screen.queryByRole("button", { name: "Link barcode to a product" })).toBeNull();
  expect(add).not.toHaveBeenCalled();
});

it("does not duplicate Enter while pending and ignores a result after outlet switching", async () => {
  let finish!: (response: Response) => void;
  const fetch = vi.fn<(url: string, options?: RequestInit) => Promise<Response>>(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const add = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrap = (ui: ReactElement) => (
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>
  );
  const { user, rerender } = renderWithProviders(
    wrap(<BarcodeInput scope={scope} canLink onProduct={add} />),
  );
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  fireEvent.submit(screen.getByLabelText("Barcode").closest("form")!);
  expect(fetch).toHaveBeenCalledTimes(1);
  const signal = fetch.mock.calls[0][1]?.signal;
  expect(new Headers(fetch.mock.calls[0][1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(
    scope.tenantId,
  );
  rerender(
    wrap(
      <BarcodeInput
        scope={{ ...scope, outletId: "0199e000-0000-7000-8000-000000000002" }}
        canLink
        onProduct={add}
      />,
    ),
  );
  expect(signal?.aborted).toBe(true);
  await act(async () => finish(new Response(JSON.stringify(product))));
  expect(add).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Barcode")).toHaveValue("");
});

it("asks an editor to link when the operator cannot write the catalogue", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "barcode_unknown", message: "Barcode not linked" }), {
        status: 404,
      }),
    ),
  );
  const { user } = renderWithProviders(
    <BarcodeInput scope={scope} canLink={false} onProduct={vi.fn()} />,
  );
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  expect(await screen.findByText(/Ask a catalogue editor/)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Link barcode to a product" })).toBeNull();
});

it("blocks scanning during payment holds and cancels an outstanding lookup", async () => {
  let finish!: (response: Response) => void;
  const fetch = vi.fn<(url: string, options?: RequestInit) => Promise<Response>>(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const add = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrap = (ui: ReactElement) => (
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>
  );
  const { user, rerender } = renderWithProviders(
    wrap(<BarcodeInput scope={scope} canLink onProduct={add} />),
  );
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  rerender(wrap(<BarcodeInput scope={scope} canLink disabled onProduct={add} />));
  expect(screen.getByLabelText("Barcode")).toBeDisabled();
  expect(fetch.mock.calls[0][1]?.signal?.aborted).toBe(true);
  await act(async () => finish(new Response(JSON.stringify(product))));
  expect(add).not.toHaveBeenCalled();
});

it("preserves the selected product after a failed link so the same link can be retried", async () => {
  let attempts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/catalogue/products"))
        return new Response(
          JSON.stringify({ data: [product], page: { has_more: false, next_cursor: null } }),
        );
      if (init?.method === "PUT") {
        attempts++;
        return attempts === 1
          ? new Response(
              JSON.stringify({
                error: "unavailable",
                message: "Service unavailable; retry the link",
              }),
              { status: 503 },
            )
          : new Response(JSON.stringify(product));
      }
      return new Response(
        JSON.stringify({ error: "barcode_unknown", message: "Barcode not linked" }),
        { status: 404 },
      );
    }),
  );
  const add = vi.fn().mockResolvedValue("Added Test product");
  const { user } = renderWithProviders(<BarcodeInput scope={scope} canLink onProduct={add} />);
  await user.type(screen.getByLabelText("Barcode"), "4006381333931{Enter}");
  await user.click(await screen.findByRole("button", { name: "Link barcode to a product" }));
  await chooseOption(user, await screen.findByLabelText("Link to product"), /TEST/);
  await user.click(screen.getByRole("button", { name: "Save barcode link" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/retry the link/);
  expect(screen.getByLabelText("Link to product")).toHaveTextContent(/TEST/);
  expect(screen.getByLabelText("Barcode")).toHaveValue("4006381333931");
  await user.click(screen.getByRole("button", { name: "Save barcode link" }));
  await waitFor(() => expect(add).toHaveBeenCalledTimes(1));
  expect(attempts).toBe(2);
});
