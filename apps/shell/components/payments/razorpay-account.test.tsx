import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { RazorpayAccount } from "./razorpay-account";

afterEach(() => vi.unstubAllGlobals());

const empty = { configured: false, key_secret_set: false, webhook_secret_set: false };
const saved = {
  configured: true,
  mode: "test",
  key_id: "rzp_test_abc",
  key_secret_set: true,
  webhook_secret_set: true,
  updated_at: "2026-10-29T10:00:00Z",
  webhook_path: "/v1/payments/webhooks/razorpay/t/a",
};

function stub(account: object, put?: () => Response) {
  const fetcher = vi.fn(async (_url: string, init?: RequestInit) =>
    init?.method === "PUT" && put ? put() : new Response(JSON.stringify(account), { status: 200 }),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it("asks for every key on the first save and sends only what was typed", async () => {
  const fetcher = stub(empty, () => new Response(JSON.stringify(saved), { status: 200 }));
  const { user } = renderWithProviders(<RazorpayAccount />);
  await user.type(await screen.findByLabelText("Key id"), "rzp_live_abc");
  await user.click(screen.getByRole("button", { name: "Save Razorpay account" }));
  expect(await screen.findByText("A test account uses an rzp_test_ key")).toBeTruthy();
  expect(screen.getByText("Enter the secret for this key id")).toBeTruthy();
  expect(screen.getByText("Enter the webhook secret")).toBeTruthy();
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);

  await user.clear(screen.getByLabelText("Key id"));
  await user.type(screen.getByLabelText("Key id"), "rzp_test_abc");
  await user.type(screen.getByLabelText("Key secret"), "good-secret");
  await user.type(screen.getByLabelText("Webhook secret"), "hook-secret");
  await user.click(screen.getByRole("button", { name: "Save Razorpay account" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/payments/razorpay-account",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          mode: "test",
          key_id: "rzp_test_abc",
          key_secret: "good-secret",
          webhook_secret: "hook-secret",
        }),
      }),
    ),
  );
});

it("shows a saved account as set, never its secrets, and keeps them when left blank", async () => {
  const fetcher = stub(saved, () => new Response(JSON.stringify(saved), { status: 200 }));
  const { user } = renderWithProviders(<RazorpayAccount />);
  const status = await screen.findByLabelText("Account status");
  expect(within(status).getAllByText("Set")).toHaveLength(2);
  expect(within(status).getByText(/Not exposed in this deployment/)).toBeTruthy();
  expect((screen.getByLabelText("Key secret") as HTMLInputElement).value).toBe("");
  await user.click(screen.getByRole("button", { name: "Save Razorpay account" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/payments/razorpay-account",
      expect.objectContaining({ body: JSON.stringify({ mode: "test", key_id: "rzp_test_abc" }) }),
    ),
  );
});

it("explains a refusal in the cashier's words, not the server's", async () => {
  stub(
    saved,
    () =>
      new Response(
        JSON.stringify({
          error: "service_unavailable",
          message: "encrypted credential storage is not configured",
        }),
        {
          status: 503,
        },
      ),
  );
  const { user } = renderWithProviders(<RazorpayAccount />);
  await user.click(await screen.findByRole("button", { name: "Save Razorpay account" }));
  expect(
    await screen.findByText(
      "Encrypted storage isn't available in this deployment, so keys can't be saved.",
    ),
  ).toBeTruthy();
});

it("names a key pair Razorpay refused", async () => {
  stub(
    saved,
    () =>
      new Response(JSON.stringify({ error: "credentials_rejected", message: "no" }), {
        status: 422,
      }),
  );
  const { user } = renderWithProviders(<RazorpayAccount />);
  await user.type(await screen.findByLabelText("Key secret"), "wrong-secret");
  await user.click(screen.getByRole("button", { name: "Save Razorpay account" }));
  expect(await screen.findByText("Razorpay didn't accept this key id and secret.")).toBeTruthy();
});
