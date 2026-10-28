// The chrome's persisted layout flag, and the <html> data attribute it is
// mirrored to. The server cannot read localStorage, so its HTML always renders
// the default (navigation collapsed); the attribute lets styles.css size the
// chrome to the stored state from the first paint, before React hydrates
// (#1091 D7). The page panel no longer collapses (#1286 D4), so an old
// `hms.panel.open` value is deliberately not read.
export const NAVIGATION_KEY = "hms.navigation.open";

export const CHROME_FLAG_ATTRIBUTES: Record<string, string> = {
  [NAVIGATION_KEY]: "hmsNavigation",
};

export function flagAttributeValue(open: boolean) {
  return open ? "open" : "closed";
}

// Inlined as a raw <script> in the root layout, because an effect would run
// after the browser has already painted the defaults. Static, package-authored,
// no user input. A missing key is left unset, so the server-rendered default
// stands; storage that throws (private browsing, disabled storage) does the
// same.
/**
 * The unsaved-edits Back guard's listener (#1303 D5). On window, popstate
 * listeners run in the order they were added, and the App Router adds its own
 * as the app shell mounts, which can be before the workspace code loads. The
 * guard must run first to stop it, so its listener is added here, before any
 * app script; navigation-guard.tsx switches what it does.
 */
export const BACK_GUARD_KEY = "__hmsBackGuard";
const BACK_GUARD_INIT = `window.${BACK_GUARD_KEY}={on:null};window.addEventListener("popstate",function(e){var g=window.${BACK_GUARD_KEY}.on;if(g)g(e);},true);`;

export const CHROME_INIT_SCRIPT = `(function(){try{var d=document.documentElement.dataset;var f=${JSON.stringify(CHROME_FLAG_ATTRIBUTES)};for(var k in f){var s=localStorage.getItem(k);if(s!==null)d[f[k]]=s==="1"?"open":"closed";}}catch(e){}})();${BACK_GUARD_INIT}`;
