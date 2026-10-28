import type { Metadata } from "next";
import { headers } from "next/headers";
import { ApplicationProviders } from "@/components/application-providers";
import { CHROME_INIT_SCRIPT } from "@helivanta/ui";
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
        {/* Must run before paint: stamps the chrome's stored panel and
            navigation state on <html> (#1091 D7) so a collapsed panel is never
            drawn open first. CHROME_INIT_SCRIPT is a static, package-authored
            constant (no user input), so sanitizeHtml does not apply —
            DOMPurify strips <script> outright. The stamped attributes differ
            from the server render, which suppressHydrationWarning on <html>
            scopes away from just that element. The app is light only (#1255),
            so no theme script runs. */}
        {/* eslint-disable-next-line no-restricted-syntax */}
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: CHROME_INIT_SCRIPT }} />
        <ApplicationProviders>
          <StyleNonce nonce={nonce} />
          <SessionRenewal />
          {children}
        </ApplicationProviders>
      </body>
    </html>
  );
}
