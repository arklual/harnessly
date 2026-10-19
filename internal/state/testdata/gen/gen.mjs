// Runs a scripted sequence of layout and workspace operations through the previous runtime's own stores and writes
// what each step left on disk, returned, broadcast and asked of tmux to ../golden.json. The Go test runs the same steps
// and must match byte for byte.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> node --no-warnings --import ./register.mjs gen.mjs
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const FIXED = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'state-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;
const A = path.join(root, 'proj-a');
const B = path.join(root, 'proj-b');
const C = path.join(root, 'proj-c');
for (const d of [A, B, C, path.join(A, 'sub')]) fs.mkdirSync(d, { recursive: true });

globalThis.__golden = { events: [], calls: [], reconciles: [], tmux: { sessions: new Set(), cwd: '/live/cwd', command: 'bash' } };
const G = globalThis.__golden;

const { dataDir } = await import('@/lib/data-dir');
const ls = await import('@/lib/layout-store');
const ws = await import('@/lib/workspace-store');
const { claudeProvider } = await import('@/lib/providers/claude');
const { codexProvider } = await import('@/lib/providers/codex');
const DATA = dataDir();
ls.setLayoutReconciler({
  reconcileWorkspaceTabs: (id, tabs) => G.reconciles.push(['tabs', id, [...tabs]]),
  removeWorkspaceTabs: (id) => G.reconciles.push(['removed', id]),
});

const scrub = (s) => s.split(root).join('$ROOT');
const snapshot = () => {
  const files = {};
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.isFile()) files[path.relative(DATA, p)] = scrub(fs.readFileSync(p, 'utf8'));
    }
  };
  walk(DATA);
  return files;
};

const steps = [];
const step = async (name, fn) => {
  G.events = []; G.calls = []; G.reconciles = [];
  let result;
  try {
    const r = await fn();
    result = r === undefined ? null : r;
  } catch (err) {
    result = { error: err.message };
  }
  await new Promise((r) => setTimeout(r, 30));
  steps.push({
    name,
    result: scrub(JSON.stringify(result)),
    events: G.events,
    reconciles: G.reconciles,
    calls: G.calls.map((c) => c.map((x) => (typeof x === 'string' ? scrub(x) : x))),
    files: snapshot(),
  });
};

const layoutOf = (id) => JSON.parse(fs.readFileSync(path.join(DATA, 'workspaces', id, 'layout.json'), 'utf8'));
const panes = (node) => (node.type === 'pane' ? [node] : [...panes(node.children[0]), ...panes(node.children[1])]);
const ids = {};

await step('init empty', () => ws.initWorkspaceStore());
await step('create workspace', async () => { const w = await ws.createWorkspace(A, ' Alpha '); ids.w1 = w.id; return w; });
await step('create second workspace', async () => { const w = await ws.createWorkspace(B); ids.w2 = w.id; return w; });
await step('create in a missing folder', () => ws.createWorkspace(path.join(A, 'missing')));
await step('create on a file', () => { fs.writeFileSync(path.join(root, 'file'), 'x'); return ws.createWorkspace(path.join(root, 'file')); });
await step('get existing layout', async () => { const l = await ls.getLayout(ids.w1); ids.p1 = l.root.id; ids.t1 = l.root.tabs[0].id; return l; });
await step('split', async () => { const l = await ls.splitPaneInLayout(ids.w1, ids.p1, 'horizontal', path.join(A, 'sub')); ids.p2 = l.activePaneId; return l; });
await step('split a browser', async () => { const l = await ls.splitPaneInLayout(ids.w1, ids.p2, 'vertical', undefined, 'web-browser'); ids.p3 = l.activePaneId; return l; });
await step('split a missing pane', () => ls.splitPaneInLayout(ids.w1, 'pane-nope', 'vertical'));
await step('add agent tab', async () => { const t = await ls.addTabToPane(ids.w1, ids.p1, '  Named  ', A, 'claude-code'); ids.tc = t.id; ids.sc = t.sessionName; return t; });
await step('add tab with command', async () => { const t = await ls.addTabToPane(ids.w1, ids.p1, undefined, undefined, undefined, 'echo hi'); ids.t3 = t.id; return t; });
await step('add tab to a missing pane', () => ls.addTabToPane(ids.w1, 'pane-nope'));
await step('panel type rename to browser', () => ls.patchTab(ids.w1, ids.p1, ids.t3, { panelType: 'web-browser' }));
await step('panel type rename back', () => ls.patchTab(ids.w1, ids.p1, ids.t3, { panelType: 'terminal' }));
await step('patch tab view and live fields', () => ls.patchTab(ids.w1, ids.p1, ids.t1, { cwd: '/body/cwd', title: 'body title', terminalRatio: 40, terminalCollapsed: true, webUrl: 'http://localhost:3000', lastCommand: 'npm test', name: 'Main' }));
await step('patch missing tab', () => ls.patchTab(ids.w1, ids.p1, 'tab-nope', { name: 'x' }));
await step('ratio', () => ls.patchLayout(ids.w1, { ratioUpdate: { path: [], ratio: 70 } }));
await step('equalize', () => ls.patchLayout(ids.w1, { equalize: true }));
await step('equalize again is no write', () => ls.patchLayout(ids.w1, { equalize: true }));
await step('diff settings and active pane', () => ls.patchLayout(ids.w1, { diffSettings: { viewMode: 'unified', panelOpen: true }, activePaneId: ids.p1 }));
await step('active pane missing', () => ls.patchLayout(ids.w1, { activePaneId: 'pane-nope' }));
await step('pane active tab', () => ls.patchPane(ids.w1, ids.p1, { activeTabId: ids.t1 }));
await step('pane active tab missing', () => ls.patchPane(ids.w1, ids.p1, { activeTabId: 'tab-nope' }));
await step('reorder tabs', () => ls.reorderTabsInPane(ids.w1, ids.p1, [ids.t3, ids.tc, ids.t1]));
await step('reorder tabs not covering', () => ls.reorderTabsInPane(ids.w1, ids.p1, [ids.t3, 'tab-x']));
await step('move tab', () => ls.moveTabBetweenPanes(ids.w1, ids.t3, ids.p1, ids.p2, 0));
await step('cli status', () => ls.updateTabCliStatus(ids.sc, 'busy', undefined, 'prompt-submit'));
await step('cli status unchanged', () => ls.updateTabCliStatus(ids.sc, 'busy', undefined, 'prompt-submit'));
await step('cli status dismissed', () => ls.updateTabCliStatus(ids.sc, 'idle', 1760000000000, null));
await step('last user message', () => ls.updateTabLastUserMessage(ids.sc, 'привет "мир"\n'));
await step('claude agent state', () => ls.updateTabAgentState(ids.sc, claudeProvider, { sessionId: 'sid-1', jsonlPath: '/t/x.jsonl', summary: 'sum', lastUserMessage: 'q' }));
await step('claude session cleared', () => ls.updateTabAgentSessionId(ids.sc, claudeProvider, null));
await step('claude summary', () => ls.updateTabAgentSummary(ids.sc, claudeProvider, 'new sum'));
await step('recovered session', () => ls.storeRecoveredAgentSession(ids.sc, claudeProvider, { sessionId: '425055ec-bbc8-4e3c-87d0-36fa74c1cccb', jsonlPath: '/p/425055ec-bbc8-4e3c-87d0-36fa74c1cccb.jsonl' }));
await step('recovered session again', () => ls.storeRecoveredAgentSession(ids.sc, claudeProvider, { sessionId: '425055ec-bbc8-4e3c-87d0-36fa74c1cccb', jsonlPath: '/p/425055ec-bbc8-4e3c-87d0-36fa74c1cccb.jsonl' }));
await step('codex replaces a foreign agent state', () => ls.updateTabAgentState(ids.sc, codexProvider, { sessionId: 'c-1', summary: null }));
await step('writer for a missing session', () => ls.updateTabLastUserMessage('tk-ws-nope-pane-a-tab-b', 'x'));
await step('close pane with focus', () => ls.closePaneInLayout(ids.w1, ids.p2));
await step('close missing pane', () => ls.closePaneInLayout(ids.w1, 'pane-nope'));
await step('remove tab', () => ls.removeTabFromPane(ids.w1, ids.p1, ids.t1));
await step('remove missing tab', () => ls.removeTabFromPane(ids.w1, ids.p1, 'tab-nope'));
await step('restart a live tab', () => ls.restartTabSession(ids.w1, ids.p1, ids.tc, 'claude'));
await step('restart with a lost folder', async () => {
  G.tmux.cwd = null;
  await ls.patchTab(ids.w1, ids.p1, ids.tc, { cwd: path.join(A, 'gone', 'deeper'), lastCommand: 'npm start' });
  G.tmux.cwd = '/live/cwd';
  G.tmux.sessions.delete(ids.sc);
  return ls.restartTabSession(ids.w1, ids.p1, ids.tc, 'npm start');
});
await step('restart with its folder', async () => { G.tmux.sessions.delete(ids.sc); return ls.restartTabSession(ids.w1, ids.p1, ids.tc, 'claude --resume'); });
await step('first pane tabs', () => ls.getFirstPaneTabs(ids.w1));
await step('group', async () => { const g = await ws.createGroup(' Team '); ids.g1 = g.id; return g; });
await step('group without a name', async () => { const g = await ws.createGroup('  '); ids.g2 = g.id; return g; });
await step('workspace into a group', () => ws.setWorkspaceGroup(ids.w2, ids.g1));
await step('reorder workspaces', () => ws.reorderWorkspaces([{ id: ids.w2 }, { id: ids.w1, groupId: ids.g2 }]));
await step('reorder workspaces bad', () => ws.reorderWorkspaces([{ id: ids.w2 }]));
await step('rename workspace', () => ws.renameWorkspace(ids.w1, 'Alpha 2'));
await step('active and sidebar', () => ws.updateActive({ activeWorkspaceId: ids.w2, sidebarWidth: 300, sidebarCollapsed: true }));
await step('active unchanged', () => ws.updateActive({ activeWorkspaceId: ids.w2 }));
await step('reorder groups', () => ws.reorderGroups([ids.g2, ids.g1]));
await step('collapse group', () => ws.setGroupCollapsed(ids.g1, true));
await step('rename group', () => ws.renameGroup(ids.g2, ' Ops '));
await step('ungroup', () => ws.ungroupGroup(ids.g1));
await step('workspaces view', () => ws.getWorkspaces());
await step('third workspace', async () => { const w = await ws.createWorkspace(C, 'Gamma'); ids.w3 = w.id; return w; });
await step('delete workspace', () => ws.deleteWorkspace(ids.w2));
await step('delete missing workspace', () => ws.deleteWorkspace('ws-nope'));
await step('legacy workspaces file', () => {
  const file = path.join(DATA, 'workspaces.json');
  const data = JSON.parse(fs.readFileSync(file, 'utf8'));
  data.workspaces[0].directory = '/legacy';
  delete data.workspaces[0].directories;
  data.workspaces[0].order = 3;
  data.workspaces[1].groupId = 'grp-gone';
  data.workspaces[1].extra = { kept: [1, 'two'] };
  data.groups[0].order = 1;
  data.custom = 'kept';
  fs.writeFileSync(file, JSON.stringify(data, null, 2));
  ws.workspacesChangedByCluster();
  return ws.updateActive({ sidebarWidth: 250 });
});
await step('layout with unknown fields', async () => {
  const file = path.join(DATA, 'workspaces', ids.w1, 'layout.json');
  const data = layoutOf(ids.w1);
  data.custom = { b: 1, a: [true, null] };
  data.root.children[0].tabs[0].extraField = 'kept';
  data['10'] = 'ten';
  data['2'] = 'two';
  fs.writeFileSync(file, JSON.stringify(data));
  await ls.layoutChangedByCluster(ids.w1);
  return ls.patchPane(ids.w1, ids.p1, { activeTabId: ids.tc });
});
await step('garbled layout', async () => {
  fs.writeFileSync(path.join(DATA, 'workspaces', ids.w3, 'layout.json'), '{"root": ');
  return ls.getLayout(ids.w3, C);
});
await step('cross-check at start', async () => {
  const l = layoutOf(ids.w1);
  G.tmux.sessions.delete(ids.sc);
  G.tmux.sessions.add(`tk-${ids.w1}-pane-zz-tab-orphan`);
  G.tmux.sessions.add(`pt-${ids.w1}-old`);
  G.tmux.sessions.add('tk-shell-tab-x');
  G.tmux.sessions.add(`tk-${ids.w3}-pane-q-tab-q`);
  return panes(l.root).length;
});
await step('init with tmux', () => ws.initWorkspaceStore());
await step('remove layout', () => ls.removeLayoutFile(ids.w3));

const out = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'golden.json');
fs.writeFileSync(out, JSON.stringify({ steps }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${steps.length} steps → ${out}`);
