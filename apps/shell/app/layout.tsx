import type { Metadata } from "next";
import { headers } from "next/headers";
import { AppProviders } from "@helivanta/api";
import { CHROME_INIT_SCRIPT, THEME_INIT_SCRIPT } from "@helivanta/ui";
import { SessionRenewal } from "@/components/session-renewal";
import { StyleNonce } from "@/components/style-nonce";
import "./globals.css";

export const metadata: Metadata = { title: "Helivanta" };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // Reading the request makes every page dynamic, which a per-request nonce
  // requires (#1113, spec D3). Fail closed: a page rendered without the nonce
  // would have every script blocked by the policy, or carry no policy at all.
  const nonce = (await headers()).get("x-nonce");
  if (!nonce) throw new Error("missing CSP nonce: middleware did not run");
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
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        {/* Same reasoning as the theme script above, for the chrome's stored
            panel and navigation state (#1091 D7): stamped on <html> before
            paint so a collapsed panel is never drawn open first. Static and
            package-authored, so sanitizeHtml does not apply. */}
        {/* eslint-disable-next-line no-restricted-syntax */}
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: CHROME_INIT_SCRIPT }} />
        <AppProviders>
          <StyleNonce nonce={nonce} />
          <SessionRenewal />
          {children}
        </AppProviders>
      </body>
    </html>
  );
}
