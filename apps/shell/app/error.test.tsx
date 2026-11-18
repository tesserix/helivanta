import { afterEach, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ErrorBoundary from "./error";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("shows a safe message and correlates its reference with sanitised reporting", async () => {
  const privateDetail = "Synthetic private 1706: SELECT credential FROM patient_records";
  const fetchReport = vi.fn(async () => new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchReport);
  const logged = vi.spyOn(console, "error").mockImplementation(() => {});
  const reset = vi.fn();
  const user = userEvent.setup();
  render(<ErrorBoundary error={new Error(privateDetail)} reset={reset} />);

  expect(screen.getByRole("main")).toBeInTheDocument();
  expect(screen.queryByText(privateDetail)).not.toBeInTheDocument();
  expect(screen.getByText(/The page could not be loaded/)).toBeInTheDocument();
  const reference = (await screen.findByText(/^HV-[0-9a-f-]{36}$/)).textContent!;
  expect(logged).toHaveBeenCalledWith(expect.stringContaining(reference));
  expect(JSON.stringify(logged.mock.calls)).not.toContain(privateDetail);
  expect(fetchReport).toHaveBeenCalledOnce();
  const [url, options] = fetchReport.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toBe("/api/client-errors");
  expect(JSON.parse(String(options.body))).toMatchObject({ reference, source: "root" });
  expect(String(options.body)).not.toContain(privateDetail);
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(reset).toHaveBeenCalledOnce();
});

it("keeps a logged support reference available when the collector is offline", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new Error("Synthetic unavailable collector");
    }),
  );
  const logged = vi.spyOn(console, "error").mockImplementation(() => {});
  render(<ErrorBoundary error={new Error("Synthetic private failure")} reset={() => {}} />);

  const reference = (await screen.findByText(/^HV-[0-9a-f-]{36}$/)).textContent!;
  expect(logged).toHaveBeenCalledWith(expect.stringContaining(reference));
  expect(screen.queryByText("Synthetic private failure")).not.toBeInTheDocument();
});

it("does not show a previous reference while a different failure is being reported", async () => {
  let release: ((response: Response) => void) | undefined;
  const fetchReport = vi.fn(async () => new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchReport);
  vi.spyOn(console, "error").mockImplementation(() => {});
  const { rerender } = render(
    <ErrorBoundary error={new Error("first private detail")} reset={() => {}} />,
  );
  const first = (await screen.findByText(/^HV-[0-9a-f-]{36}$/)).textContent!;
  fetchReport.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  rerender(<ErrorBoundary error={new Error("second private detail")} reset={() => {}} />);
  expect(screen.queryByText(first)).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Preparing support reference");
  release!(new Response(null, { status: 204 }));
  expect((await screen.findByText(/^HV-[0-9a-f-]{36}$/)).textContent).not.toBe(first);
});

it("keeps recovery usable when both diagnostic sinks fail", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new Error("private transport detail");
    }),
  );
  vi.spyOn(console, "error").mockImplementation(() => {
    throw new Error("private logger detail");
  });
  render(<ErrorBoundary error={new Error("private original detail")} reset={() => {}} />);
  expect(
    await screen.findByText("Support reference unavailable. Please try again."),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
  expect(screen.queryByText(/private .* detail/)).not.toBeInTheDocument();
});
