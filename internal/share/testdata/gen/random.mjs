// Generated cases of the pure parts of the previous runtime's snapshots, for the Go test's SHARE_RANDOM run: redaction
// of strings full of secrets and paths, capture of random timelines, the economics of random transcripts, and the page
// and Markdown of random snapshots (their Markdown messages included). Each case holds its input and what the old code
// returned.

export const randomCases = ({ n, seed, redact, snap, render, home, workspace, profileHomes, env, cliToken, dropHost }) => {
  let s = seed >>> 0;
  const rnd = () => {
    s = (s + 0x6d2b79f5) | 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const pick = (a) => a[Math.floor(rnd() * a.length)];
  const int = (k) => Math.floor(rnd() * k);
  const chars = (alphabet, len) => Array.from({ length: len }, () => alphabet[int(alphabet.length)]).join('');
  const AZ = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  const az = 'abcdefghijklmnopqrstuvwxyz';
  const D = '0123456789';
  const W = AZ + az + D + '_-';
  const B64 = AZ + az + D + '+/=';
  const secrets = () => pick([
    `-----BEGIN ${pick(['RSA ', 'EC ', 'OPENSSH ', ''])}PRIVATE KEY-----\n${chars(B64, 40)}\n-----END ${pick(['RSA ', ''])}PRIVATE KEY-----`,
    `eyJ${chars(W, 10 + int(20))}.${chars(W, 10 + int(20))}.${chars(W, 9 + int(20))}`,
    `sk-ant-api03-${chars(W, 18 + int(20))}`, `sk-${pick(['proj-', ''])}${chars(W, 18 + int(20))}`,
    `gh${pick(['p', 'o', 'u', 's', 'r', 'x'])}_${chars(AZ + az + D, 34 + int(6))}`, `github_pat_${chars(W, 20 + int(10))}`,
    `glpat-${chars(W, 18 + int(10))}`, `xox${pick(['a', 'b', 'p', 'r', 's', 'z'])}-${chars(W, 8 + int(10))}`,
    `${chars(D, 7 + int(4))}:${chars(W, 34 + int(3))}`, `AKIA${chars(AZ + D, 15 + int(3))}`,
    `${pick(['aws_secret_access_key', 'AWS_SECRET_ACCESS_KEY', 'aws_secret_access_key"'])}${pick(['=', ': ', ' = ', ':'])}${pick(['', '"', "'"])}${chars(AZ + az + D + '/+=', 38 + int(4))}`,
    `AIza${chars(W, 33 + int(4))}`, `t1.${chars(W, 5)}=.${chars(W, 86)}`, `AQVN${chars(W, 34 + int(6))}`, `y0_${chars(W, 48 + int(5))}`,
    `${pick(['Authorization', 'authorization', '"Authorization"'])}${pick([': ', ':'])}${pick(['Bearer ', 'Basic ', 'Token ', 'bearer '])}${chars(W + '.', 5 + int(30))}`,
    `Bearer ${chars(W + '._~+/=', 14 + int(10))}`, `${pick(['Cookie', 'Set-Cookie', 'cookie'])}: ${pick(['a=b; c=d', '', '  [скрыто: x]'])}`,
    `${pick(['postgres', 'postgresql', 'mysql', 'mongodb+srv', 'redis', 'rediss', 'amqp', 'ftp'])}://${pick(['u', 'user.name', ''])}:${pick(['p4ss', 'p@ss', '[скрыто: x]', ''])}@db:5432/x`,
    `${pick(['http', 'https', 'HTTPS'])}://${pick(['u', 'me'])}:${pick(['pw', 'p/w', 'x'])}@host/p`,
    `${pick(['', '  ', 'export '])}${pick(['API_KEY', 'MY_SECRET', 'GITHUB_TOKEN', 'DB_PASSWORD', 'PASS', 'SENTRY_DSN', 'key', 'NOT_IT'])}=${pick(['value123', '', ' x', '"[скрыто: y]"', 'a b c'])}`,
    `${pick(['password', 'Passwd', 'secret', 'token', 'api_key', 'apiKey', 'api-key', 'access_key', 'client_secret', 'CLIENT-SECRET'])}${pick(['', '"', "'"])}${pick(['=', ': ', ':'])}${pick(['', '"', "'"])}${chars(W + '!@#', 3 + int(10))}`,
    chars(B64 + '_-', 30 + int(20)), chars('0123456789abcdef', 40), chars(az + D + '/._-', 34),
    env.API_TOKEN, env.DEPLOY_KEY, cliToken, `${env.API_TOKEN}${env.DEPLOY_KEY}`,
    `${home}${pick(['', '/', '/x/y', '.bak', 'x'])}`, `${workspace}/${pick(['src/a.go', '', 'b'])}`, `/home/${chars(az + '._-', 1 + int(8))}${pick(['', '/z', '!'])}`,
    `/root${pick(['', '/', 'x', '/.ssh'])}`, `${profileHomes}/${chars(az, 3)}/home${pick(['', '/w', 'x'])}`, `http://home/x`, `~/home/x`, `a/home/b`,
    String.fromCharCode(0x202a + int(5)), String.fromCharCode(0x2066 + int(4)), '[скрыто: jwt]', ' ', '\r\n', ' ', '🙂',
  ]);
  const prose = () => Array.from({ length: 1 + int(8) }, () => (rnd() < 0.4 ? secrets() : pick(['файл', 'the', 'build', 'ok', '—', 'x=1', ':', '"', "'", ' ', '\n', '\t', 'Готово.', '`code`', '**жирный**', 'https://example.com/a?b=1', `https://ai.${dropHost}/agents/x`, `https://${dropHost}/agents`, `https://x.${dropHost}/other`]))).join(pick([' ', '', '\n', '  ']));

  const mdBlock = () => pick([
    () => `${'#'.repeat(1 + int(6))} ${prose()}`,
    () => Array.from({ length: 1 + int(4) }, (_, i) => `${pick(['-', '*', `${i + 1}.`])} ${pick(['', '[ ] ', '[x] '])}${prose()}`).join('\n'),
    () => `\`\`\`${pick(['', 'ts', 'bash', 'diff'])}\n${prose()}\n\`\`\``,
    () => `| a | b |\n|${pick([':-', '-', '-:'])}|${pick([':-:', '-'])}|\n| ${prose().replace(/\n|\|/g, ' ')} | ${pick(['x', '`y`', '[l](https://example.com)'])} |`,
    () => `> ${prose().split('\n').join('\n> ')}`,
    () => `[${pick(['link', 'ссылка', '`c`'])}](${pick(['https://example.com/a(b)', `https://ai.${dropHost}/agents/t`, '/rel', '#x', 'mailto:a@example.com', 'javascript:alert(1)', 'https://пример.рф/п', 'http://127.1:80/a/../b', 'www.example.com'])}) ${prose()}`,
    () => `![${pick(['', 'alt', 'Скрин'])}](https://example.com/i.png)`,
    () => `${prose()}[^1]\n\n[^1]: ${prose()}`,
    () => pick(['---', '<details>\n<summary>x</summary>\n\nтекст\n</details>', '<br>', '&copy; &amp; &#x27;']),
    () => prose(),
  ])();
  const markdown = () => Array.from({ length: 1 + int(5) }, mdBlock).join(pick(['\n\n', '\n']));
  const ts = () => pick([1792315800000, 1792315800000 + int(1e10), 1700000000000 + int(1e9), 1792315859999, 1792353599000]);
  const status = () => pick(['success', 'error', 'pending', undefined]);
  const diff = () => pick([
    { kind: 'replace', file: `${workspace}/${pick(['a.go', 'b c.ts'])}`, old: pick(['', prose()]), new: prose() },
    { kind: 'patch', text: pick(['--- a\n+++ b\n@@\n-x\n+y', '', ' plain\n-old\n+new\n```']) },
  ]);
  const shareEntry = () => {
    const at = ts();
    switch (int(6)) {
      case 0: return { t: 'user', at, text: prose(), images: pick([0, 0, 1, 2, 5, 11, 21, 22]) };
      case 1: return { t: 'assistant', at, markdown: markdown(), model: pick([null, 'claude-x', 'codex-y']) };
      case 2: return { t: 'thinking', at, text: prose() };
      case 3: return { t: 'plan', at, markdown: markdown() };
      case 4: return { t: 'note', at, text: prose() };
      default: {
        const e = { t: 'action', at, name: pick(['Read', 'Grep', 'Bash', 'Edit', 'Write', 'apply_patch', 'exec', 'WebSearch', 'Agent', 'Task', 'docs.search', 'Glob', 'NotebookEdit', 'MultiEdit']), summary: prose(), status: status() };
        if (rnd() < 0.3) e.diff = diff();
        return e;
      }
    }
  };
  const economics = () => ({
    tokens: { input: pick([0, 5, 999, 1000, 1499, 1550, 12345, 999949, 999950, 1e6, 1234567, 1.5e9, 9.99e12]), output: int(5000), cacheRead: int(100000), cacheWrite: pick([0, 1, 777]) },
    activeMs: pick([null, 0, 29999, 30000, 59999, 60000, 3600000, 3630000, 5 * 3600000 + 59 * 60000 + 31000]),
    wallMs: pick([0, -5, 89999, 90000, 7200000, 1e9]),
    turns: pick([0, 1, 2, 5, 11, 12, 21, 101, 112]),
  });
  const snapshot = () => ({
    schema: 1, title: pick(['', prose(), 'Заголовок\nс переносом', `"<b>&'`]), provider: pick(['claude', 'codex']), model: pick([null, '', 'model-a', prose()]),
    owner: { sub: pick([null, 'alice@example.com', '']) }, capturedAt: ts(), firstAt: null, lastAt: null,
    options: { tools: 'summary', thinking: true, diffs: true, economics: true }, economics: rnd() < 0.5 ? null : economics(),
    entries: Array.from({ length: int(12) }, shareEntry),
  });
  const timelineEntry = () => {
    const timestamp = ts();
    switch (int(16)) {
      case 0: return { type: 'user-message', timestamp, text: pick([prose(), '<system-reminder>x</system-reminder>  ', `<pasted_content a="1">${prose()}</pasted_content>`, '<command-name>/x</command-name>', `<local-command-stdout>${prose()}</local-command-stdout>keep`]), ...(rnd() < 0.3 ? { images: Array.from({ length: int(3) }, () => ({})) } : {}), ...(rnd() < 0.1 ? { pending: true } : {}), ...(rnd() < 0.1 ? { attachmentPlaceholder: true } : {}) };
      case 1: case 2: return { type: 'assistant-message', timestamp, markdown: pick([markdown(), '   ', 'x'.repeat(20001 + int(10))]), ...(rnd() < 0.7 ? { model: pick(['m-1', null, '']) } : {}) };
      case 3: return { type: 'thinking', timestamp, thinking: pick([prose(), ' ']) };
      case 4: return { type: 'reasoning-summary', timestamp, summary: pick([[], [prose()], [prose(), prose()]]) };
      case 5: return { type: 'tool-call', timestamp, toolName: pick(['Bash', 'Edit', 'Read']), summary: pick([prose(), 'a'.repeat(450)]), status: status(), ...(rnd() < 0.5 ? { diff: { filePath: `${workspace}/f.go`, oldString: prose(), newString: prose() } } : {}) };
      case 6: return { type: 'agent-group', timestamp, agentType: pick(['Explore', '', undefined]), description: pick(['Разведка', '']) };
      case 7: return { type: 'task-notification', timestamp, summary: prose(), status: pick(['completed', 'failed', 'running']) };
      case 8: return { type: 'exec-command-stream', timestamp, command: prose(), status: status() };
      case 9: return { type: 'web-search', timestamp, ...(rnd() < 0.8 ? { query: prose() } : {}), status: status() };
      case 10: return { type: 'mcp-tool-call', timestamp, server: 'docs', tool: 'search', ...(rnd() < 0.8 ? { argumentsSummary: prose() } : {}), status: status() };
      case 11: return { type: 'patch-apply', timestamp, files: [{ path: `${workspace}/a.go` }, { path: 'b.go' }], status: status(), ...(rnd() < 0.5 ? { diff: prose() } : {}) };
      case 12: return { type: 'plan', timestamp, markdown: markdown() };
      case 13: return { type: 'ask-user-question', timestamp, questions: [{ question: prose() }, { question: 'Да?' }], ...(rnd() < 0.5 ? { answer: prose() } : {}) };
      case 14: return pick([{ type: 'interrupt', timestamp }, { type: 'context-compacted', timestamp }, { type: 'error-notice', timestamp, severity: pick(['error', 'warning']), message: prose() },
        { type: 'slash-command', timestamp, name: 'model', args: pick(['', 'opus']), ...(rnd() < 0.2 ? { pending: true } : {}) }]);
      default: return { type: pick(['tool-result', 'turn-end', 'session-exit', 'approval-request']), timestamp };
    }
  };
  const transcript = (provider) => Array.from({ length: int(20) }, () => {
    const t = pick(['2026-10-18T09:00:00.000Z', '2026-10-18T10:30:15.250Z', 'bad', 5, undefined, '2026-10-17']);
    if (provider === 'claude') {
      return pick([
        () => JSON.stringify({ type: 'assistant', timestamp: t, isSidechain: pick([false, true, undefined]), message: { id: pick(['m1', 'm2', 'm3', '', 0, 7, null]), usage: pick([{ input_tokens: int(500), output_tokens: int(50), cache_read_input_tokens: pick([0, 1e4, 1.5, -2, '3']), cache_creation_input_tokens: int(9) }, null, 5]) } }),
        () => JSON.stringify({ type: 'system', subtype: pick(['turn_duration', 'x']), durationMs: pick([1000, 0, '5', null, 2.5]), timestamp: t }),
        () => JSON.stringify({ type: 'user', timestamp: t, message: { content: 'hi' } }),
        () => pick(['', '{bad', '[1,2]', '"str"', '42', '{"type":"assistant"}']),
      ])();
    }
    return pick([
      () => JSON.stringify({ type: 'event_msg', timestamp: t, payload: { type: 'token_count', info: pick([null, 'x', { total_token_usage: { input_tokens: int(9000), cached_input_tokens: int(12000), output_tokens: int(300) } }, { total_token_usage: null }]) } }),
      () => JSON.stringify({ type: 'event_msg', timestamp: t, payload: { type: 'task_complete', ...(rnd() < 0.6 ? { duration_ms: pick([1000, 0, 2.5, '7']) } : {}) } }),
      () => JSON.stringify({ type: pick(['response_item', 'turn_context']), timestamp: t, payload: { type: 'x' } }),
      () => JSON.stringify({ type: 'event_msg', timestamp: t, payload: pick([null, 5, []]) }),
      () => pick(['', '{bad', '[]']),
    ])();
  }).join('\n') + pick(['', '\n']);

  const context = () => ({
    knownSecrets: [env.API_TOKEN, env.DEPLOY_KEY, cliToken, ...(rnd() < 0.3 ? ['short', 'tok-golden-value-123-longer'] : [])],
    homeDirs: [home, pick([home, home + '/', '', '/'])],
    workspaceDir: pick([workspace, workspace + '/', null, '']),
  });
  const cases = [];
  for (let i = 0; i < n; i++) {
    const kind = pick(['redact', 'redact', 'capture', 'economics', 'render', 'render', 'render']);
    const ctx = context();
    if (kind === 'redact') {
      const r = redact.createRedactor(ctx);
      const inputs = Array.from({ length: 1 + int(4) }, prose);
      const outputs = inputs.map((x) => r.redact(x));
      cases.push({ kind, ctx, inputs, outputs, report: r.report() });
    } else if (kind === 'capture') {
      const options = { tools: pick(['summary', 'none']), thinking: rnd() < 0.5, diffs: rnd() < 0.5, economics: rnd() < 0.5 };
      const timeline = Array.from({ length: int(15) }, timelineEntry);
      const econ = rnd() < 0.5 ? null : economics();
      const r = redact.createRedactor(ctx);
      const input = { provider: pick(['claude', 'codex']), title: prose(), sub: pick([null, 'alice@example.com']), now: ts() };
      const res = snap.captureSnapshot({ timeline, provider: input.provider, title: input.title, owner: { sub: input.sub }, options, economics: econ, redactor: r, now: input.now });
      cases.push({ kind, ctx, options, timeline, economics: econ, input, result: res });
    } else if (kind === 'economics') {
      const provider = pick(['claude', 'codex']);
      const content = transcript(provider);
      let result;
      try { result = snap.readTranscriptEconomics(content, provider); } catch { result = 'THROWS'; }
      cases.push({ kind, provider, content, result });
    } else {
      const snapshotValue = snapshot();
      const mode = pick(['preview', 'export', 'shared']);
      const viewer = pick([null, 'bob@example.com', '<x>']);
      const expiresAt = pick([null, ts()]);
      let html;
      let md;
      try {
        html = render.renderSnapshotHtml(snapshotValue, { redactor: redact.createRedactor(ctx), mode, viewer, expiresAt });
        md = render.renderSnapshotMarkdown(snapshotValue, redact.createRedactor(ctx));
      } catch (e) {
        html = `THROWS ${e}`;
      }
      cases.push({ kind, ctx, snapshot: snapshotValue, mode, viewer, expiresAt, html, md });
    }
  }
  // Every case's input went through JSON before the Go side sees it; a value JSON drops (undefined) is dropped here too.
  return JSON.parse(JSON.stringify(cases));
};
