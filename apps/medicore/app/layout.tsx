import type { Metadata } from "next";
import { AppProviders } from "@hms/api";
import "./globals.css";

export const metadata: Metadata = { title: "HMS" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="default">
      <body>
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
