// The fixtures the fake external services answer with, and the requests of the golden run. Both are data the Go test
// reads back from ../fixtures.json and ../golden.json.

const G = 'gitlab.example.com';
const P = 'gitlab.com';
const T = 'tracker.example.com';
const SELF = ['private-token', 'self-file'];
const PUB = ['private-token', 'pub-file'];
const YT = ['authorization', 'Bearer yt-file'];
const API = '/api/v4/projects/';
const MRS = (branch) => `/merge_requests?source_branch=${encodeURIComponent(branch)}&state=all&per_page=3&order_by=updated_at`;
const PIPES = (branch) => `/pipelines?ref=${encodeURIComponent(branch)}&per_page=1`;
const json = (v, extra = {}) => ({ status: 200, headers: { 'content-type': 'application/json' }, body: JSON.stringify(v), ...extra });
const missing = (extra = {}) => ({ status: 404, headers: { 'content-type': 'application/json' }, body: '{"message":"404 Not Found"}', ...extra });
const html = (body, extra = {}) => ({ status: 200, headers: { 'content-type': 'text/html; charset=utf-8' }, body, ...extra });
const png = (n) => Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, ...Array.from({ length: n }, (_, i) => (i * 7) % 256)]).toString('base64');
const pipe = (status, id) => ({ status, web_url: `https://${G}/group/alpha/-/pipelines/${id}` });

const ISSUE_FIELDS = 'idReadable,summary,resolved,customFields(name,value(name,login,fullName,avatarUrl)),parent(issues(idReadable,summary,customFields(name,value(name))))';
const TASK_FIELDS = encodeURIComponent([
  'idReadable', 'summary', 'description', 'resolved', 'updated', 'project(shortName)', 'parent(issues(idReadable,summary))',
  'subtasks(issues(idReadable,summary,resolved,customFields(name,value(name))))', 'customFields(name,value(name,login,fullName,avatarUrl))',
  'comments(author(login,fullName),created,text)',
].join(','));

export const fixtureSet = (sha) => {
  const f = {};
  const at = (host, target, v) => { f[`${host} ${target}`] = v; };
  const g = (target, v) => at(G, API + target, { auth: SELF, ...v });
  const p = (target, v) => at(P, API + target, { auth: PUB, ...v });

  // alpha: two session branches, a merge request found through its branch.
  g('group%2Falpha', json({ id: 1, path_with_namespace: 'group/alpha' }));
  g('group%2Falpha/repository/branches/main', json({ name: 'main', commit: { id: sha('alpha', 'main~1'), title: 'feat: one', committed_date: '2026-10-18T10:30:00.000+00:00' } }));
  g(`group%2Falpha${PIPES('main')}`, json([pipe('running', 11)]));
  g(`group%2Falpha${MRS('main')}`, json([]));
  g('group%2Falpha/repository/branches/feature%2Fx', json({ commit: { id: sha('alpha', 'feature/x'), title: 'fix: three', committed_date: '2026-10-18T10:50:00.000+00:00' } }));
  g(`group%2Falpha${PIPES('feature/x')}`, json([pipe('waiting_for_resource', 12)]));
  g(`group%2Falpha${MRS('feature/x')}`, json([
    { iid: 7, title: 'Feature X (list)', state: 'opened', web_url: `https://${G}/group/alpha/-/merge_requests/7`, head_pipeline: null },
    { iid: 5, title: 'Old <one> & "quoted"', state: 'merged', draft: true, web_url: `https://${G}/group/alpha/-/merge_requests/5` },
  ]));
  g('group%2Falpha/merge_requests/7', json({
    iid: 7, title: 'Feature X', state: 'opened', draft: false, web_url: `https://${G}/group/alpha/-/merge_requests/7`,
    head_pipeline: pipe('success', 13), description: 'Does X.\r\n\n- one\n- two', author: { name: 'Agent Smith' },
    source_branch: 'feature/x', target_branch: 'main', updated_at: '2026-10-18T11:00:00.000Z', changes_count: '4',
  }));
  g('group%2Falpha/merge_requests/7/diffs?per_page=100', json([
    { old_path: 'src/a.txt', new_path: 'src/a.txt', new_file: false, deleted_file: false, renamed_file: false, diff: '@@ -1 +1 @@\n-a\n+b\n' },
    { old_path: 'dev/null', new_path: 'new.txt', new_file: true, deleted_file: false, renamed_file: false, diff: '@@ -0,0 +1 @@\n+new' },
    { old_path: 'gone.txt', new_path: 'gone.txt', new_file: false, deleted_file: true, renamed_file: false, diff: '@@ -1 +0,0 @@\n-old\n' },
    { old_path: 'old/name.txt', new_path: 'new/name.txt', new_file: false, deleted_file: false, renamed_file: true, diff: '' },
    { old_path: 'img.png', new_path: 'img.png', new_file: false, deleted_file: false, renamed_file: false, diff: '', too_large: true },
    { old_path: 'x.txt', new_path: 'x.txt', new_file: false, deleted_file: false, renamed_file: false, diff: '--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-1\n+2\n\n' },
  ]));
  g('group%2Falpha/merge_requests/5', missing());
  g('group%2Falpha/merge_requests/404', missing());
  g('group%2Falpha/merge_requests/404/diffs?per_page=100', missing());
  g('group%2Falpha/merge_requests/8', json({ iid: 8, title: 'Huge', state: 'merged', draft: true, web_url: `https://${G}/group/alpha/-/merge_requests/8`, changes_count: '1000+', description: null }));
  g('group%2Falpha/merge_requests/8/diffs?per_page=100', { status: 200, headers: { 'content-type': 'application/json' }, gen: { kind: 'files', count: 40, lines: 900 } });
  g('group%2Falpha/merge_requests/9', json({ iid: 9, title: 'No diffs', state: 'closed', web_url: `https://${G}/group/alpha/-/merge_requests/9`, head_pipeline: { status: 'failed' } }));
  g('group%2Falpha/merge_requests/9/diffs?per_page=100', missing());
  g('group%2Falpha/repository/commits/1cb8a0a4', json({
    id: sha('alpha', 'feature/x'), title: 'fix: three', message: 'fix: three\n\n  Longer body\nwith lines.  \n', author_name: 'Agent',
    created_at: '2026-10-18T10:50:00.000+00:00', web_url: `https://${G}/group/alpha/-/commit/${sha('alpha', 'feature/x')}`, last_pipeline: pipe('manual', 14),
  }));
  g('group%2Falpha/repository/commits/1cb8a0a4/diff?per_page=100', json([
    { old_path: 'src/deep/b.txt', new_path: 'src/deep/b.txt', new_file: false, deleted_file: false, renamed_file: false, diff: '@@ -1 +1,2 @@\n two\n+three\n' },
  ]));
  g('group%2Falpha/repository/commits/abcdef0', json({ id: 'abcdef0123456789', title: 'Other title', message: 'Not the title', web_url: `https://${G}/group/alpha/-/commit/abcdef0` }));
  g('group%2Falpha/repository/commits/abcdef0/diff?per_page=100', json(Array.from({ length: 100 }, (_, i) => ({ old_path: `f${i}`, new_path: `f${i}`, diff: `@@ -1 +1 @@\n+${i}\n` }))));

  // beta: renamed on the public GitLab, so its old path redirects to the project's id.
  p('team%2Fsub%2Fbeta', { status: 301, headers: { location: `https://${P}/api/v4/projects/42` } });
  p('42', json({ id: 42, path_with_namespace: 'team/sub/beta-renamed' }));
  p('team%2Fsub%2Fbeta-renamed/repository/branches/trunk', missing());
  p(`team%2Fsub%2Fbeta-renamed${PIPES('trunk')}`, json([]));
  p(`team%2Fsub%2Fbeta-renamed${MRS('trunk')}`, json([{ iid: 3, title: 'Beta docs (list)', state: 'opened', web_url: `https://${P}/team/sub/beta-renamed/-/merge_requests/3` }]));
  p('team%2Fsub%2Fbeta-renamed/merge_requests/3/diffs?per_page=100', json([]));
  p('team%2Fsub%2Fbeta-renamed/merge_requests/3', json({ iid: 3, title: 'Beta docs', state: 'locked', draft: 1, web_url: `https://${P}/team/sub/beta-renamed/-/merge_requests/3`, head_pipeline: { status: 'created', web_url: `https://${P}/p/1` } }));

  // inner and epsilon.
  g('group%2Finner', json({ path_with_namespace: 'group/inner' }));
  g('group%2Finner/repository/branches/main', json({ commit: { id: 'feedface', title: 'pushed elsewhere', committed_date: '2026-10-18T13:00:00.000+00:00' } }));
  g(`group%2Finner${PIPES('main')}`, json({ 0: { status: 'success', web_url: 'https://ci.example.com/1' } }));
  g(`group%2Finner${MRS('main')}`, json([]));
  g('group%2Fepsilon', json({ path_with_namespace: 'group/epsilon' }));
  g('group%2Fepsilon/repository/branches/local-only', missing());
  g(`group%2Fepsilon${PIPES('local-only')}`, missing());
  g(`group%2Fepsilon${MRS('local-only')}`, json([]));

  // Merge requests named in the conversation.
  g('group%2Fother', missing());
  g('group%2Fother/merge_requests/12', json({ iid: 12, title: 'Other', state: 'merged', web_url: `https://${G}/group/other/-/merge_requests/12` }));
  g('group%2Fz', missing());
  g('group%2Fz/merge_requests/2', json({ iid: 2, title: 'Zed', state: 'opened', draft: true, web_url: `https://${G}/group/z/-/merge_requests/2` }));
  g('broken%2Fjson', { status: 200, headers: { 'content-type': 'application/json' }, body: 'not json' });
  g('gone%2Fproject', { drop: true });
  g('gone%2Fproject/merge_requests/4', json({ iid: 4, title: 'Survived', state: 'opened', web_url: `https://${G}/gone/project/-/merge_requests/4` }));

  // Link previews: merge requests read without following redirects.
  g('group%2Falpha/merge_requests/70', json({ iid: 70, title: 'Preview', state: 'weird', draft: 0 }));
  p('team%2Fsub%2Fbeta/merge_requests/3', { status: 301, headers: { location: `https://${P}/api/v4/projects/42/merge_requests/3` } });

  // The tracker.
  const yt = (target, v) => at(T, target, { auth: YT, ...v });
  const assignee = { name: 'Assignee', value: { name: 'jdoe', login: 'jdoe', fullName: 'John  Doe', avatarUrl: '/hub/api/rest/avatar/abc?dpr=2' } };
  yt(`/api/issues/ACME-12?fields=${ISSUE_FIELDS}`, json({
    idReadable: 'ACME-12', summary: 'Fix the thing', resolved: null,
    customFields: [{ name: 'State', value: { name: 'In Progress' } }, assignee, { name: 'Type', value: { name: 'Task' } }, { name: 'Priority', value: null }],
    parent: { issues: [{ idReadable: 'ACME-1', summary: 'The epic', customFields: [{ name: 'Type', value: { name: 'Epic' } }] }] },
  }));
  yt(`/api/issues/DEMO-3?fields=${ISSUE_FIELDS}`, json({
    idReadable: 'DEMO-3', summary: 'Under an idea', resolved: 1700000000000,
    customFields: [{ name: 'Assignee', value: { login: 'asmith', name: 'asmith' } }, { name: 'Type', value: { name: 'ИДЕЯ' } }],
    parent: { issues: [{ idReadable: 'DEMO-1', summary: 'Idea', customFields: [{ name: 'Type', value: { name: 'Идея' } }] }] },
  }));
  yt(`/api/issues/APP-4?fields=${ISSUE_FIELDS}`, missing());
  yt(`/api/issues/APP-6?fields=${ISSUE_FIELDS}`, json({ idReadable: 'APP-6', summary: 'Bad fields', customFields: { State: 1 } }));
  yt(`/api/issues/APP-7?fields=${ISSUE_FIELDS}`, json({ idReadable: 'APP-7', summary: 7, customFields: [{ name: 'Assignee', value: 'nobody' }], parent: { issues: 'x' } }));
  yt('/api/admin/projects?fields=shortName,archived&$top=200', json([
    { shortName: 'ACME' }, { shortName: 'DEMO', archived: false }, { shortName: 'OLD', archived: true }, { shortName: 'APP' }, { shortName: 'x1' }, { shortName: 'TOOLONGNAME1' },
  ]));
  yt('/hub/api/rest/avatar/abc?dpr=2', { status: 200, headers: { 'content-type': 'image/png' }, bodyB64: png(40) });
  yt('/api/users?query=asmith&fields=login,avatarUrl&$top=10', json([{ login: 'asmith2', avatarUrl: '/hub/api/rest/avatar/no' }, { login: 'asmith', avatarUrl: `https://${T}/hub/api/rest/avatar/zzz` }]));
  yt('/hub/api/rest/avatar/zzz', { status: 200, headers: { 'content-type': 'image/svg+xml' }, body: '<svg xmlns="http://www.w3.org/2000/svg"/>' });
  yt('/api/users?query=nobody&fields=login,avatarUrl&$top=10', json([]));
  yt('/api/users?query=evil&fields=login,avatarUrl&$top=10', json([{ login: 'evil', avatarUrl: 'https://other.example.com/hub/api/rest/avatar/x' }]));
  yt('/api/users?query=weird&fields=login,avatarUrl&$top=10', json([{ login: 'weird', avatarUrl: '/hub/api/rest/avatar/weird' }]));
  yt('/hub/api/rest/avatar/weird', html('<p>not an image</p>'));
  yt('/api/users?query=odd&fields=login,avatarUrl&$top=10', json({ login: 'odd' }));
  yt('/api/users?query=big&fields=login,avatarUrl&$top=10', json([{ login: 'big', avatarUrl: '/hub/api/rest/avatar/big' }]));
  yt('/hub/api/rest/avatar/big', { status: 200, headers: { 'content-type': 'image/gif' }, gen: { kind: 'bytes', count: 200001 } });

  yt(`/api/issues/ACME-12?fields=${TASK_FIELDS}`, json({
    idReadable: 'ACME-12', summary: '  Fix\tthe\n thing  ', description: 'Line one\r\nLine two\rthree \u001b[31mred\u001b[0m \u0007bell',
    resolved: null, updated: 1792321500123, project: { shortName: 'ACME' },
    parent: { issues: [{ idReadable: 'ACME-1', summary: 'The epic' }] },
    subtasks: { issues: [
      { idReadable: 'ACME-13', summary: 'Child', resolved: 5, customFields: [{ name: 'State', value: { name: 'Done' } }] },
      { idReadable: 'OTHER-1', summary: 'Foreign' }, null,
      { idReadable: 'APP-2', summary: '', customFields: [] },
    ] },
    customFields: [assignee, { name: 'Type', value: { name: 'Bug' } }, { name: 'State', value: { name: 'Open' } }, { name: 'Priority', value: { name: 'Major' } }],
    comments: Array.from({ length: 12 }, (_, i) => ({ author: i % 3 ? { login: `u${i}`, fullName: i % 2 ? `User ${i}` : '' } : null, created: 1792321500000 + i, text: `comment ${i}${'x'.repeat(i === 11 ? 5000 : 0)}` })),
  }));
  yt(`/api/issues/DEMO-404?fields=${TASK_FIELDS}`, missing());
  yt(`/api/issues/APP-5?fields=${TASK_FIELDS}`, { status: 500, body: 'oops' });
  yt(`/api/issues/APP-8?fields=${TASK_FIELDS}`, json({ idReadable: 'OTHER-8', summary: 'Wrong space' }));
  yt(`/api/issues/APP-9?fields=${TASK_FIELDS}`, json({ idReadable: 'APP-9', summary: 'Minimal' }));

  // Published pages.
  const D = 'docs.example.com';
  at(D, '/a/report-1', html(`<html><head><TITLE lang="en">  Report &amp; notes &#x41;&#66; &laquo;q&raquo; &bogus; \n</title>
<meta property="og:description" content="A &quot;report&quot;&nbsp;about &#0; things"></head><body>x</body></html>`));
  at(D, '/b/board-2/?k=key_1', html('<title>Board two</title><meta name="description" content=\'single quoted\'>'));
  at(D, '/a/plain', { status: 200, headers: { 'content-type': 'text/plain' }, body: '<title>no</title>' });
  at(D, '/a/notitle', html('<p>no title</p>'));
  at(D, '/a/moved', { status: 302, headers: { location: '/a/report-1' } });

  // Sites and their icons.
  const I = 'icons.example.org';
  at(I, '/', html(`<head><link rel="icon" href="/favicon-32.png" sizes="16x16 32x32"><LINK REL="apple-touch-icon" href="https://cdn.example.org/touch.png">
<link rel="icon" type="image/svg+xml" href="icon.svg#x"><link rel="stylesheet" href="/s.css"><link rel="icon" href="http://insecure.example.org/i.png">
<link rel="shortcut icon" href="data:image/png;base64,AAAA"></head>`));
  at(I, '/icon.svg', { status: 200, headers: { 'content-type': 'image/svg+xml' }, body: '<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>' });
  at('cdn.example.org', '/touch.png', { status: 200, headers: { 'content-type': 'image/png' }, bodyB64: png(10) });
  at(I, '/favicon-32.png', { status: 200, headers: { 'content-type': 'image/png' }, bodyB64: png(20) });
  at('sub.blank.example.org', '/', missing());
  at('sub.blank.example.org', '/favicon.ico', missing());
  at('blank.example.org', '/', html('<link rel="icon" href="/notimage.png"><link rel="icon" href="//cdn.example.org/huge.png" sizes="64x64">'));
  at('blank.example.org', '/notimage.png', html('<p>html</p>'));
  at('cdn.example.org', '/huge.png', { status: 200, headers: { 'content-type': 'image/png' }, gen: { kind: 'bytes', count: 200001 } });
  at('blank.example.org', '/favicon.ico', { status: 200, headers: { 'content-type': 'image/x-icon' }, bodyB64: png(5) });
  at('none.example.org', '/', missing());
  at('none.example.org', '/favicon.ico', missing());
  at('example.org', '/', missing());
  at('example.org', '/favicon.ico', missing());
  at('moved.example.net', '/', html('<link rel="apple-touch-icon" href="/old-touch.png">'));
  at('moved.example.net', '/old-touch.png', { status: 301, headers: { location: 'https://cdn.example.org/touch.png' } });
  return f;
};

const ses = { cookie: 'theme=dark; session-token=good' };
const cli = { 'x-cli-token': 'cli-secret' };
const jsonType = { 'content-type': 'application/json' };
const R = '/api/platform';
const gitlabBody = (jsonlPath, mrs = []) => JSON.stringify({
  paths: [
    '$ROOT/work/alpha/src/deep/b.txt', '$ROOT/work/alpha/src/a.txt', '$ROOT/work/alpha', 'relative/path', '$ROOT/work/alpha/src/deep/b.txt',
    '$ROOT/work/beta/notes.md', '$ROOT/work/gamma/w.txt', '$ROOT/work/delta/d.txt', '$ROOT/work/delta/vendor/inner/i.txt',
    '$ROOT/work/epsilon/e.txt', '$ROOT/work/missing/file.txt', 7, null,
  ],
  jsonlPath, mrs,
});
const MENTIONED = [
  `https://${G}/group/alpha/-/merge_requests/7`, `https://${G}/group/other/-/merge_requests/12#note_1`,
  `https://${P}/team/sub/beta/-/merge_requests/3`, 'https://evil.example.com/x/-/merge_requests/1', 'not a url',
  `https://${G}:8443/group/alpha/-/merge_requests/9`, 'https://GITLAB.example.com/group/z/-/merge_requests/2/diffs', 42,
  `https://${G}/gone/project/-/merge_requests/4`,
];

const workspaces = JSON.stringify({
  workspaces: [
    { id: 'ws-a', name: 'Alpha', directory: '/home/agent/a', order: 2, groupId: 'g-gone' },
    { id: 'ws-b', name: 'Beta', directories: ['/home/agent/b'], groupId: 'g-1', extra: { kept: true } },
  ],
  groups: [{ id: 'g-1', name: 'One', order: 1 }], activeWorkspaceId: 'ws-b', sidebarCollapsed: true, updatedAt: '2026-01-01T00:00:00.000Z',
}, null, 2);
const config = JSON.stringify({ authPassword: 'scrypt:x', authSecret: 's', appTheme: 'dark', hasAuthPassword: 'kept place', '2': 'two', updatedAt: '2026-01-01T00:00:00.000Z' });

export const cases = [
  // The page's start data.
  {
    setup: { files: {
      'home/.acme-data/workspaces.json': workspaces, 'home/.acme-data/config.json': config,
      'home/.acme-data/quick-prompts.json': JSON.stringify({ custom: [{ id: 'c1', name: 'Mine', prompt: 'do it', enabled: false }, null, 'ab'], disabledBuiltinIds: ['builtin-simplify', 3], order: ['c1', 'builtin-commit'] }),
      'home/.acme-data/sidebar-items.json': JSON.stringify([{ id: 'x' }]),
    } },
    method: 'GET', url: `${R}/bootstrap`, headers: ses,
  },
  { method: 'GET', url: `/agents${R}/bootstrap/`, headers: { ...cli, 'if-none-match': 'ETAG' } },
  { method: 'POST', url: `${R}/bootstrap`, headers: ses },
  { method: 'GET', url: `${R}/bootstrap`, headers: {} },
  {
    setup: { files: {
      'home/.acme-data/config.json': '{"broken"', 'home/.acme-data/quick-prompts.json': '{"disabledBuiltinIds":"builtin-commit","custom":null,"order":5}',
      'home/.acme-data/sidebar-items.json': JSON.stringify({ disabledBuiltinIds: ['builtin-stats'], custom: [{ id: 's1', name: 'Site', icon: 'Globe', url: 'https://example.com', enabled: true }], order: ['s1'] }),
      'home/.acme-data/workspaces.json': JSON.stringify({ workspaces: [{ id: 'ws-c', name: 'C', directories: [] }, 5], groups: 'none' }),
    } },
    method: 'GET', url: `${R}/bootstrap`, headers: ses,
  },
  {
    setup: { files: { 'home/.acme-data/config.json': '[1,2]', 'home/.acme-data/quick-prompts.json': '{"disabledBuiltinIds":{"a":1}}', 'home/.acme-data/sidebar-items.json': '{"custom":"x"}' } },
    method: 'GET', url: `${R}/bootstrap`, headers: ses,
  },
  {
    setup: { files: { 'home/.acme-data/config.json': 'false', 'home/.acme-data/quick-prompts.json': '[{"id":"builtin-commit","enabled":false},{"id":"mine","name":"M","prompt":"p","enabled":true}]', 'home/.acme-data/sidebar-items.json': null } },
    method: 'GET', url: `${R}/bootstrap`, headers: ses,
  },

  // GitLab: a session's pushes and merge requests.
  {
    setup: {
      files: {
        'home/.config/acme/youtrack-token': 'yt-file\n', 'home/.config/acme/gitlab-self-token': '  self-file  \n',
        'home/.config/acme/gitlab-token': '', 'home/.config/acme/glab-token': 'pub-file',
        'home/.claude/projects/p/session.jsonl': '{"type":"summary"}\nnot json\nnull\n{"timestamp":"bad"}\n{"timestamp":"2026-10-18T10:00:00.000Z"}\n',
        'home/.claude/projects/p/late.jsonl': '\n  \n{"timestamp":"2026-10-18T12:15:00Z","x":1}\n',
        'home/.codex/sessions/2026/x.jsonl': '{"timestamp":1792317600000.9}\n',
        'home/.claude/projects/p/far.jsonl': '{"timestamp":1e20}\n',
        'home/outside.jsonl': '{"timestamp":"2026-10-18T10:00:00.000Z"}\n',
      },
    },
    method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.claude/projects/p/session.jsonl', MENTIONED),
  },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.claude/projects/p/late.jsonl') },
  { method: 'POST', url: `/agents${R}/gitlab/`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.codex/sessions/2026/x.jsonl') },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/outside.jsonl', MENTIONED.slice(0, 3)) },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.claude/projects/p/../p/session.jsonl') },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.claude/projects/p/far.jsonl') },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: JSON.stringify({ paths: ['$ROOT/work/alpha'], jsonlPath: null, mrs: [] }) },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: JSON.stringify({ mrs: [`https://${G}/broken/json/-/merge_requests/1`] }) },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, 'content-type': 'text/plain' }, body: 'paths' },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: '{"paths":' },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: '' },
  { method: 'GET', url: `${R}/gitlab`, headers: ses },
  { method: 'POST', url: `${R}/gitlab`, headers: { ...cli, ...jsonType }, body: '{}' },
  { method: 'POST', url: `${R}/gitlab`, headers: jsonType, body: '{}' },
  {
    setup: { env: { GITLAB_SELF_TOKEN: 'self-env' } },
    method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType }, body: gitlabBody('$ROOT/home/.claude/projects/p/session.jsonl', MENTIONED),
  },
  { setup: { env: { GITLAB_SELF_TOKEN: null } }, method: 'POST', url: `${R}/gitlab`, headers: { ...ses, ...jsonType, 'if-none-match': 'ETAG' }, body: JSON.stringify({ mrs: [] }) },

  // GitLab view.
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/7`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/7/diffs?x=1`)}`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/8`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/9`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/404`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/commit/1cb8a0a4`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/group/alpha/-/commit/abcdef0/`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${P}/team/sub/beta/-/merge_requests/3`)}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent('https://example.com/group/alpha/-/merge_requests/7')}`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view?url=a&url=b`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view`, headers: ses },
  { method: 'POST', url: `${R}/gitlab-view`, headers: ses },
  { method: 'GET', url: `${R}/gitlab-view`, headers: cli },
  { method: 'GET', url: `${R}/gitlab-view?url=${encodeURIComponent(`https://${G}/broken/json/-/merge_requests/1`)}`, headers: ses },

  // Link previews.
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent(`https://${T}/issue/ACME-12/fix-the-thing?x#y`)}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://youtrack.example.com/youtrack/issue/DEMO-3')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=ACME-12`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=ACME-12`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/link-meta?key=${encodeURIComponent(' APP-4 ')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=ZZZ-1`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=OLD-1`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=APP-6`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=APP-7`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent(`https://${G}/group/alpha/-/merge_requests/70`)}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent(`https://${P}/team/sub/beta/-/merge_requests/3/diffs`)}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent(`https://${G}/group/../alpha/-/merge_requests/70`)}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/a/report-1')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/b/board-2/?x=1&k=key_1#f')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://platform.example.com/agents/docs/report-1/')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/b/api')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/a/report-1?k=bad key')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/a/plain')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/a/notitle')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('https://docs.example.com/a/moved')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=${encodeURIComponent('http://docs.example.com/a/report-1')}`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?url=a&url=b&key=ACME-12`, headers: ses },
  { method: 'GET', url: `${R}/link-meta`, headers: ses },
  { method: 'DELETE', url: `${R}/link-meta`, headers: ses },
  { method: 'GET', url: `${R}/link-meta?key=ACME-12`, headers: cli },
  { method: 'GET', url: `${R}/link-meta/projects`, headers: ses },
  { method: 'POST', url: `${R}/link-meta/projects`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/link-meta/projects`, headers: cli },

  // Tracker avatars.
  { method: 'GET', url: `${R}/link-meta/avatar?login=jdoe`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=jdoe`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/link-meta/avatar?login=asmith`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=nobody`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=nobody`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=evil`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=weird`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=big`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=odd`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=bad!`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=a&login=b`, headers: ses },
  { method: 'HEAD', url: `${R}/link-meta/avatar?login=asmith`, headers: ses },

  // Task cards.
  { method: 'GET', url: `${R}/task?key=ACME-12`, headers: ses },
  { method: 'GET', url: `${R}/task?key=ACME-12`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/task?key=DEMO-404`, headers: ses },
  { method: 'GET', url: `${R}/task?key=APP-5`, headers: ses },
  { method: 'GET', url: `${R}/task?key=APP-8`, headers: ses },
  { method: 'GET', url: `${R}/task?key=APP-9`, headers: ses },
  { method: 'GET', url: `${R}/task?key=NOPE-1`, headers: ses },
  { method: 'GET', url: `${R}/task?key=ACME-1&key=ACME-2`, headers: ses },
  { method: 'PUT', url: `${R}/task?key=ACME-12`, headers: ses },
  { setup: { env: { YOUTRACK_TOKEN: ' yt-env ' } }, method: 'GET', url: `${R}/task?key=APP-9`, headers: ses },
  { method: 'GET', url: `${R}/task?key=ACME-12`, headers: ses },
  { setup: { env: { YOUTRACK_TOKEN: null }, files: { 'home/.config/acme/youtrack-token': null } }, method: 'GET', url: `${R}/task?key=APP-77`, headers: ses },
  { method: 'GET', url: `${R}/link-meta/avatar?login=newperson`, headers: ses },
  { setup: { files: { 'home/.config/acme/youtrack-token': 'yt-file' } }, method: 'GET', url: `${R}/task?key=APP-9`, headers: ses },

  // Site icons.
  { method: 'GET', url: `${R}/favicon?host=icons.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=ICONS.example.org`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'GET', url: `${R}/favicon?host=sub.blank.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=none.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=moved.example.net`, headers: ses },
  { method: 'POST', url: `${R}/favicon?host=moved.example.net`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=127.0.0.1`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=printer.local`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=-bad.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=bad-.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=localhost`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=a.example.org&host=b.example.org`, headers: ses },
  { method: 'GET', url: `${R}/favicon`, headers: ses },
  { method: 'GET', url: `${R}/favicon?host=icons.example.org`, headers: cli },
  { method: 'GET', url: `${R}/favicon?host=icons.example.org`, headers: {} },

  // The organizer.
  { method: 'GET', url: `${R}/organizer`, headers: ses },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, 'content-type': 'text/plain' }, body: '{"ops":[]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '[1]' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-short","name":"x","color":"blue"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-abcdef","name":"  ","color":"blue"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-abcdef","name":"A","color":"black"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-abcdef","name":"A","color":"blue","index":-1}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"pin","ref":"agent:BAD REF"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"nope"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.update","id":"prj-abcdef"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"assign","refs":["chat:abcdefgh"]}]}' },
  {
    method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, 'content-type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ ops: [
      { op: 'project.create', id: 'prj-abcdef', name: '  Alpha \t project ', color: 'blue' },
      { op: 'project.create', id: 'prj-second1', name: 'Second', color: 'green', index: 0 },
      { op: 'pin', ref: 'agent:m1:tab-1' }, { op: 'pin', ref: 'chat:12345678', index: 0 }, { op: 'pin', ref: 'agent:tab-2', index: 99 },
      { op: 'assign', refs: ['agent:m1:tab-1', 'chat:12345678'], projectId: 'prj-abcdef' },
      { op: 'assign', refs: ['agent:7'], projectId: 'prj-second1' },
    ] }),
  },
  { method: 'GET', url: `${R}/organizer`, headers: { ...ses, 'if-none-match': 'ETAG' } },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-abcdef","name":"Alpha project","color":"teal"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.create","id":"prj-abcdef","name":"Renamed","color":"teal"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.update","id":"prj-zzzzzz","name":"x"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.reorder","ids":["prj-abcdef"]}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"project.reorder","ids":["prj-abcdef","prj-second1"]},{"op":"project.update","id":"prj-second1","color":"rose","name":" Second  2 "}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"pins.reorder","refs":["agent:tab-2"]}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"pins.reorder","refs":["agent:tab-2","agent:m1:tab-1","chat:12345678"]},{"op":"unpin","ref":"chat:12345678"}]}' },
  {
    method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType },
    body: JSON.stringify({ ops: Array.from({ length: 20 }, (_, i) => ({ op: 'pin', ref: `agent:p${i}` })) }),
  },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"assign","refs":["agent:x"],"projectId":"prj-nothere"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"forget","refs":["agent:m1:tab-1","agent:7"]},{"op":"project.delete","id":"prj-abcdef"}]}' },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"assign","refs":["agent:tab-2"],"projectId":null}]}' },
  { method: 'DELETE', url: `${R}/organizer`, headers: ses },
  { method: 'GET', url: `${R}/organizer`, headers: cli },
  { setup: { files: { 'home/.acme-data/organizer.json': '{"version":1,"revision":2.5}' } }, method: 'GET', url: `${R}/organizer`, headers: ses },
  { setup: { files: { 'home/.acme-data/organizer.json': '{"version":1,"revision":3,"updatedAt":"x","extra":{"keep":true},"projects":[{"id":"p","name":"n","color":"amber","createdAt":1,"more":2}],"assignments":{"2":"p","agent:a":"p","1":"p"},"pins":[]}' } }, method: 'GET', url: `${R}/organizer`, headers: ses },
  { method: 'PATCH', url: `${R}/organizer`, headers: { ...ses, ...jsonType }, body: '{"ops":[{"op":"assign","refs":["agent:b"],"projectId":null},{"op":"pin","ref":"agent:a"}]}' },
];
