import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import LoginPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
}));
vi.mock("firebase/auth", () => ({
  signInWithEmailAndPassword: vi.fn(),
}));
vi.mock("@/lib/firebase", () => ({ firebaseAuth: () => ({}) }));

describe("LoginPage", () => {
  it("prefills dev credentials and validates inline", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    expect(screen.getByLabelText("Email")).toHaveValue("test@hms.dev");
    await user.clear(screen.getByLabelText("Email"));
    await user.clear(screen.getByLabelText("Password"));
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  });
});
