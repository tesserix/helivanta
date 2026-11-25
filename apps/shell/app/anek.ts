import localFont from "next/font/local";

// The public pages' headline face (#1834 spec D4), shared by / and /roadmap.
export const anek = localFont({
  src: "./fonts/anek-latin.woff2",
  weight: "100 800",
  display: "swap",
  declarations: [{ prop: "font-stretch", value: "75% 125%" }],
  // Headlines only (#1834 spec D4); body text is the root layout's UI face.
  variable: "--font-anek",
});
