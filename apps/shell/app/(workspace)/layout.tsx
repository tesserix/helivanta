import { HmsShell } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";

// The one place the chrome is rendered (#1091). A layout persists across
// client-side navigation between its routes, so the rail, panel, header and
// idle tracker mount once per page load instead of once per page. Every
// authenticated route lives under this group; public routes stay outside it.
// The tenant switcher is decided here, once, so it is on every page.
export default function WorkspaceLayout({ children }: { children: React.ReactNode }) {
  return <HmsShell tenantPicker={<TenantPicker />}>{children}</HmsShell>;
}
