import type { Metadata } from "next";
import { AppProviders } from "@helivanta/api";
import { THEME_INIT_SCRIPT } from "@helivanta/ui";
import { SessionRenewal } from "@/components/session-renewal";
import "./globals.css";

export const metadata: Metadata = { title: "HMS" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="default" suppressHydrationWarning>
      <body>
        {/* Must run before hydration/paint — sets data-theme from
            localStorage/system preference synchronously so dark-mode users
            never see a flash of the light theme. THEME_INIT_SCRIPT is a
            static, package-authored constant (no user input), so
            sanitizeHtml doesn't apply here — DOMPurify strips <script>
            tags outright, which would defeat the purpose. Because this
            script intentionally stamps data-theme before hydration, the
            server-rendered "default" and the pre-hydration value diverge —
            suppressHydrationWarning on <html> above scopes React's
            hydration check away from just that attribute; children still
            get full hydration checking. */}
        {/* eslint-disable-next-line no-restricted-syntax */}
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        <AppProviders>
          <SessionRenewal />
          {children}
        </AppProviders>
      </body>
    </html>
  );
}
