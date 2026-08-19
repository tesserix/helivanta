#!/usr/bin/env node
/**
 * Helivanta statusline.
 *
 * Renders two lines: identity (model / dir / branch / cost) and telemetry
 * (context bar / graft graph state / tokens saved).
 *
 * The graft segment reuses graft's own building blocks rather than
 * reimplementing its stat resolution — the same coupling graft's generated
 * shim already had. Every graft import is optional: if the package is missing
 * or its internals move, the graft segment silently drops and the rest of the
 * line still renders. A statusline that throws shows the user nothing at all,
 * so every failure path here degrades instead of propagating.
 */
const path = require('path');
const fs = require('fs');
const { pathToFileURL } = require('url');
const { execFileSync } = require('child_process');

const dir = process.env.CLAUDE_PROJECT_DIR || process.cwd();
const BAKED = "/Users/Mahesh.Sangawar/.nvm/versions/node/v22.19.0/lib/node_modules/@nanonets/graft/dist/claude";

function fromPkg(base) {
  try {
    const pkg = require.resolve('@nanonets/graft/package.json', { paths: [base] });
    return path.join(path.dirname(pkg), 'dist', 'claude');
  } catch { return null; }
}

function globalRoot() {
  try {
    return execFileSync('npm', ['root', '-g'], {
      encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'],
      shell: process.platform === 'win32',
    }).trim() || null;
  } catch { return null; }
}

function distDir() {
  const out = [];
  if (BAKED) out.push(BAKED);
  const local = fromPkg(dir); if (local) out.push(local);
  const legacy = fromPkg(path.join(path.dirname(process.execPath), '..', 'lib')); if (legacy) out.push(legacy);
  const gr = globalRoot(); if (gr) out.push(path.join(gr, '@nanonets', 'graft', 'dist', 'claude'));
  return out.find((d) => fs.existsSync(path.join(d, 'statusline.js'))) || null;
}

const C = {
  indigo: (s) => `\x1b[38;2;84;111;255m${s}\x1b[0m`,
  amber:  (s) => `\x1b[38;2;224;165;68m${s}\x1b[0m`,
  green:  (s) => `\x1b[38;2;106;168;79m${s}\x1b[0m`,
  red:    (s) => `\x1b[38;2;204;85;68m${s}\x1b[0m`,
  muted:  (s) => `\x1b[38;5;244m${s}\x1b[0m`,
  text:   (s) => `\x1b[38;5;251m${s}\x1b[0m`,
  bold:   (s) => `\x1b[1m${s}\x1b[0m`,
};
const SEP = C.muted(' · ');

/** Context bar. Colour tracks headroom, not aesthetics: the bar exists to make
 * "running out" visible before the number is read, so the thresholds are the
 * points where behaviour should change (start being selective / start wrapping up). */
function contextBar(pct, width = 16) {
  const clamped = Math.max(0, Math.min(100, pct));
  const filled = Math.round((clamped / 100) * width);
  const colour = clamped >= 80 ? C.red : clamped >= 55 ? C.amber : C.green;
  return colour('█'.repeat(filled)) + C.muted('░'.repeat(width - filled)) + ' ' + colour(`${clamped}%`);
}

/** Branch from .git/HEAD — a file read, no subprocess, so it costs nothing on
 * every render. Detached HEAD yields a short sha instead of a name. */
function branch() {
  try {
    const head = fs.readFileSync(path.join(dir, '.git', 'HEAD'), 'utf8').trim();
    const m = head.match(/^ref: refs\/heads\/(.+)$/);
    return m ? m[1] : head.slice(0, 7);
  } catch { return null; }
}

/** Dirty marker. This is the one subprocess on the render path, so it is capped
 * hard: 250ms and untracked files excluded. A slow or huge repo loses the
 * marker rather than stalling the statusline. */
function dirty() {
  try {
    const out = execFileSync('git', ['status', '--porcelain', '--untracked-files=no'], {
      cwd: dir, encoding: 'utf8', timeout: 250, stdio: ['ignore', 'pipe', 'ignore'],
    });
    return out.trim().length > 0;
  } catch { return false; }
}

function compact(n) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}k`;
  return String(n);
}

async function graftSegments(input) {
  const d = distDir();
  if (!d) return { top: null, saved: 0 };
  try {
    const [sl, st, fmt] = await Promise.all([
      import(pathToFileURL(path.join(d, 'statusline.js')).href),
      import(pathToFileURL(path.join(d, 'state.js')).href),
      import(pathToFileURL(path.join(d, 'format.js')).href),
    ]);
    const stats = sl.resolveStats(dir);
    const session = st.readSession(dir, input.session_id || 'default');
    if (!stats || !stats.nodeCount) {
      return { top: C.muted('graft ') + C.amber('not built'), saved: 0 };
    }
    const bits = [
      C.indigo('graft ') + C.text(`${stats.nodeCount}n/${stats.edgeCount}e`),
      fmt.freshnessSegment(stats),
    ];
    return { top: bits.join(' '), saved: session?.savedTokens ?? 0 };
  } catch { return { top: null, saved: 0 }; }
}

async function main() {
  let input = {};
  try { input = JSON.parse(fs.readFileSync(0, 'utf8')); } catch { /* no/invalid stdin */ }

  // Subagents get one compact line — their own name plus the query graft ran
  // for them. Rendering the full two-line block per subagent buries the parent.
  const agent = input?.agent?.name;
  if (agent) {
    const d = distDir();
    if (d) {
      try {
        const [st, fmt] = await Promise.all([
          import(pathToFileURL(path.join(d, 'state.js')).href),
          import(pathToFileURL(path.join(d, 'format.js')).href),
        ]);
        process.stdout.write(fmt.renderSubagent(agent, st.readSession(dir, input.session_id || 'default')));
        return;
      } catch { /* fall through to the plain name */ }
    }
    process.stdout.write(C.muted('◤ ') + C.indigo(agent));
    return;
  }

  const { top: graft, saved } = await graftSegments(input);

  const line1 = [];
  const model = input?.model?.display_name;
  if (model) line1.push(C.bold(C.indigo(model)));
  const cwd = input?.workspace?.current_dir || input?.cwd || dir;
  line1.push(C.text(path.basename(cwd)));
  const b = branch();
  if (b) line1.push(C.amber(b) + (dirty() ? C.red('*') : ''));
  const cost = input?.cost?.total_cost_usd;
  if (typeof cost === 'number' && cost > 0) line1.push(C.muted(`$${cost.toFixed(2)}`));

  const line2 = [];
  const pct = input?.context_window?.used_percentage;
  if (typeof pct === 'number') line2.push(contextBar(Math.round(pct)));
  if (graft) line2.push(graft);
  if (saved > 0) line2.push(C.indigo(`~${compact(saved)} tok saved`));

  const out = [C.muted('◤ ') + line1.join(SEP)];
  if (line2.length) out.push(C.muted('▸ ') + line2.join(SEP));
  process.stdout.write(out.join('\n'));
}

main().catch(() => { /* never let the statusline throw */ });
