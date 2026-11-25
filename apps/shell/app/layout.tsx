import type { Metadata } from "next";
import { headers } from "next/headers";
import localFont from "next/font/local";
import { ApplicationProviders } from "@/components/application-providers";
import { CHROME_INIT_SCRIPT } from "@helivanta/ui";
import { SessionRenewal } from "@/components/session-renewal";
import { StyleNonce } from "@/components/style-nonce";
import "./globals.css";

// The deployed origin (#863), so the social image URL is absolute (#1835 D6).
// Previews emit it too, which is acceptable for link-preview metadata.
export const metadata: Metadata = {
  title: "Helivanta",
  metadataBase: new URL("https://helivanta.app"),
};

// The UI face (#1834 spec D4). The variable goes on <html>: Tailwind writes
// --font-sans on :root, so --font-plex must exist there or it resolves to
// nothing and the page falls back to the system face. Committed OFL files, never a build-time fetch:
// CI and the image build must not depend on a font host being reachable. One
// file per weight, each subset to Latin, Latin Extended and currency (₹).
const plex = localFont({
  src: [
    { path: "./fonts/plex/ibm-plex-sans-400.woff2", weight: "400", style: "normal" },
    { path: "./fonts/plex/ibm-plex-sans-500.woff2", weight: "500", style: "normal" },
    { path: "./fonts/plex/ibm-plex-sans-600.woff2", weight: "600", style: "normal" },
    { path: "./fonts/plex/ibm-plex-sans-700.woff2", weight: "700", style: "normal" },
  ],
  variable: "--font-plex",
  display: "swap",
});

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  // Reading the request makes every page dynamic, which a per-request nonce
  // requires (#1113, spec D3). Fail closed: a page rendered without the nonce
  // would have every script blocked by the policy, or carry no policy at all.
  const nonce = (await headers()).get("x-nonce");
  if (!nonce) throw new Error("missing CSP nonce: middleware did not run");
  return (
    <html lang="en" data-theme="default" className={plex.variable} suppressHydrationWarning>
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
