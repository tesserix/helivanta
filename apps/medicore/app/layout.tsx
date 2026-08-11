import type { Metadata } from "next";
import { AppProviders } from "@hms/api";
import { THEME_INIT_SCRIPT } from "@hms/ui";
import "./globals.css";

export const metadata: Metadata = { title: "HMS" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="default">
      <body>
        {/* Must run before hydration/paint — sets data-theme from
            localStorage/system preference synchronously so dark-mode users
            never see a flash of the light theme. THEME_INIT_SCRIPT is a
            static, package-authored constant (no user input), so
            sanitizeHtml doesn't apply here — DOMPurify strips <script>
            tags outright, which would defeat the purpose. */}
        {/* eslint-disable-next-line no-restricted-syntax */}
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
