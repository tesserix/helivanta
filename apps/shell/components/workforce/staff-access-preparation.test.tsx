import { screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StaffAccessPreparation } from "./staff-access-preparation";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce",
  useSearchParams: () => new URLSearchParams(),
}));
it("explains inactive hospital Staff and links the independent approval steps without claiming access", () => {
  renderWithProviders(
    <NavigationGuardProvider>
      <StaffAccessPreparation kind="hospital" active={false} />
    </NavigationGuardProvider>,
  );
  expect(screen.getByText(/Staff record is inactive/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Employment reviews" })).toHaveAttribute(
    "href",
    "/workforce/employment-reviews",
  );
  expect(screen.getByRole("link", { name: "Department postings" })).toHaveAttribute(
    "href",
    "/workforce/postings",
  );
  expect(screen.getByRole("link", { name: "Pharmacy relationships" })).toHaveAttribute(
    "href",
    "/workforce/pharmacy-relationships",
  );
  expect(screen.getByText(/Active directory status does not grant/)).toBeInTheDocument();
});
