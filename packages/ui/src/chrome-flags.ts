// The chrome's two persisted layout flags, and the <html> data attribute each
// one is mirrored to. The server cannot read localStorage, so its HTML always
// renders the defaults (panel open, navigation collapsed); the attributes let
// styles.css size the chrome to the stored state from the first paint, before
// React hydrates (#1091 D7).
export const PANEL_KEY = "hms.panel.open";
export const NAVIGATION_KEY = "hms.navigation.open";

export const CHROME_FLAG_ATTRIBUTES: Record<string, string> = {
  [PANEL_KEY]: "hmsPanel",
  [NAVIGATION_KEY]: "hmsNavigation",
};

export function flagAttributeValue(open: boolean) {
  return open ? "open" : "closed";
}

// Inlined as a raw <script> in the root layout, like THEME_INIT_SCRIPT and for
// the same reason: an effect would run after the browser has already painted
// the defaults. Static, package-authored, no user input. A missing key is
// left unset, so the server-rendered default stands; storage that throws
// (private browsing, disabled storage) does the same.
export const CHROME_INIT_SCRIPT = `(function(){try{var d=document.documentElement.dataset;var f=${JSON.stringify(CHROME_FLAG_ATTRIBUTES)};for(var k in f){var s=localStorage.getItem(k);if(s!==null)d[f[k]]=s==="1"?"open":"closed";}}catch(e){}})();`;
