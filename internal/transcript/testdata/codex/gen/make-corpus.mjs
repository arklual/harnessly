// Writes the synthetic Codex transcripts of ../corpus: every record kind and branch of the parser, malformed lines,
// invalid and multi-byte UTF-8, files above the parser's chunk size, and the variants the session statistics read.
// node make-corpus.mjs
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const out = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'corpus');
fs.mkdirSync(out, { recursive: true });

const UP = '/home/agent/.runtime/uploads';
let clock = Date.parse('2026-03-01T10:00:00.000Z');
const ts = () => new Date((clock += 1250)).toISOString();
const rec = (type, payload, extra = {}) => JSON.stringify({ timestamp: ts(), type, payload, ...extra });
const ev = (payload) => rec('event_msg', payload);
const ri = (payload) => rec('response_item', payload);
const msg = (role, text, phase) => ri({ type: 'message', role, content: [{ type: role === 'user' ? 'input_text' : 'output_text', text }], ...(phase ? { phase } : {}) });
const write = (name, parts) => fs.writeFileSync(path.join(out, name), Buffer.concat(parts.map((p) => (Buffer.isBuffer(p) ? p : Buffer.from(p)))));
const lines = (arr, last = '\n') => arr.join('\n') + last;

const meta = (id = '0199a000-0000-7000-8000-000000000001') =>
  rec('session_meta', { id, timestamp: '2026-03-01T09:59:59.000Z', cwd: '/home/agent/work', originator: 'codex_cli_rs', cli_version: '0.50.0' });
const turnContext = (model = 'gpt-5.4', effort = 'high') => rec('turn_context', { cwd: '/home/agent/work', model, effort, approval_policy: 'never' });
const tokenCount = (total, last, window = 272000, extra = {}) =>
  ev({ type: 'token_count', info: { total_token_usage: total, last_token_usage: last, model_context_window: window }, rate_limits: { primary: { used_percent: 12, window_minutes: 300, resets_at: 1772360000 } }, ...extra });
const usage = (input, cached, output, reasoning) => ({ input_tokens: input, cached_input_tokens: cached, output_tokens: output, reasoning_output_tokens: reasoning, total_tokens: input + output });

const basic = [
  meta(),
  turnContext(),
  ri({ type: 'message', role: 'user', content: [{ type: 'input_text', text: '# AGENTS.md instructions for /home/agent/work\n\nbe nice' }, { type: 'input_text', text: '<environment_context>\n  <cwd>/home/agent/work</cwd>\n</environment_context>' }] }),
  ri({ type: 'message', role: 'developer', content: [{ type: 'input_text', text: 'developer notes' }] }),
  ev({ type: 'task_started', model_context_window: 272000 }),
  msg('user', 'Fix the failing build\nand   explain\twhy'),
  ev({ type: 'user_message', message: 'Fix the failing build\nand   explain\twhy', images: [], local_images: [] }),
  ri({ type: 'reasoning', summary: [{ type: 'summary_text', text: '**Looking** at the build' }, '  ', 'plain string summary', { text: '   ' }, ['array item'], null, 7], encrypted_content: 'gAAAA' }),
  ri({ type: 'reasoning', summary: [], encrypted_content: '' }),
  ri({ type: 'reasoning', summary: [], encrypted_content: 'only-encrypted' }),
  ri({ type: 'reasoning', summary: 'not an array' }),
  ri({ type: 'function_call', name: 'exec_command', arguments: JSON.stringify({ cmd: 'npm run build\nsecond line', workdir: '/home/agent/work' }), call_id: 'call_exec_1' }),
  ri({ type: 'function_call_output', call_id: 'call_exec_1', output: 'line one\nline two\nline three' }),
  ri({ type: 'function_call', name: 'shell', arguments: JSON.stringify({ command: ['bash', '-lc', 'ls'] }), call_id: 'call_shell_1' }),
  ri({ type: 'function_call_output', call_id: 'call_shell_1', output: 'a\nb' }),
  ri({ type: 'function_call', name: 'write_stdin', arguments: '{"session_id":3,"chars":"y\\n"}', call_id: 'call_stdin_1' }),
  ri({ type: 'function_call', name: 'read_file', arguments: JSON.stringify({ file_path: '/home/agent/work/src/main.ts' }), call_id: 'call_read_1' }),
  ri({ type: 'function_call_output', call_id: 'call_read_1', output: 'export const x = 1;' }),
  ri({ type: 'function_call', name: 'read', arguments: '{}', call_id: 'call_read_2' }),
  ri({ type: 'function_call', name: 'list_files', arguments: JSON.stringify({ path: 'src' }), call_id: 'call_list_1' }),
  ri({ type: 'function_call', name: 'list', arguments: '[]', call_id: 'call_list_2' }),
  ri({ type: 'function_call', name: 'grep', arguments: JSON.stringify({ query: 'TODO' }), call_id: 'call_grep_1' }),
  ri({ type: 'function_call', name: 'search', arguments: JSON.stringify({ pattern: null, query: 'x' }), call_id: 'call_search_1' }),
  ri({ type: 'function_call', name: 'write_file', arguments: JSON.stringify({ path: 'out.txt', content: 'hi' }), call_id: 'call_write_1' }),
  ri({ type: 'function_call', name: 'write', arguments: 'not json at all', call_id: 'call_write_2' }),
  ri({ type: 'function_call', name: 'view_image', arguments: JSON.stringify({ path: '/tmp/screen shot.png' }), call_id: 'call_view_1' }),
  ri({ type: 'function_call', name: 'update_plan', arguments: JSON.stringify({ plan: [{ step: 'one', status: 'pending' }], explanation: 'why' }), call_id: 'call_plan_1' }),
  ri({ type: 'function_call', name: 'odd_keys', arguments: '{"b":1,"10":"ten","2":"two"}', call_id: 'call_keys_1' }),
  ri({ type: 'function_call', name: 'empty_key', arguments: '{"":"blank","x":1}', call_id: 'call_keys_2' }),
  ri({ type: 'function_call', name: 'nullargs', arguments: 'null', call_id: 'call_null_1' }),
  ri({ type: 'function_call', name: 'numargs', arguments: '42', call_id: 'call_num_1' }),
  ri({ type: 'function_call', name: 'objargs', arguments: { direct: { nested: [1, 2.5, true, null, 'é'] } }, call_id: 'call_obj_1' }),
  ri({ type: 'function_call', name: 'arrargs', arguments: '[["deep"],2]', call_id: 'call_arr_1' }),
  ri({ type: 'function_call', name: 'longargs', arguments: JSON.stringify({ text: 'word '.repeat(40) }), call_id: 'call_long_1' }),
  ri({ type: 'function_call', name: 'noargs', call_id: 'call_noargs_1' }),
  ri({ type: 'function_call', name: '', arguments: '{}', call_id: 'call_noname' }),
  ri({ type: 'function_call', name: 'nocall', arguments: '{}' }),
  ri({ type: 'function_call_output', call_id: 'call_view_1', output: '' }),
  ri({ type: 'function_call_output', call_id: 'call_plan_1', output: '   Plan updated   ' }),
  ri({ type: 'function_call_output', call_id: 'call_keys_1', output: 'x'.repeat(130) }),
  ri({ type: 'function_call_output', call_id: 'call_unknown', output: 'orphan output' }),
  ri({ type: 'function_call_output', output: 'no call id' }),
  ri({ type: 'function_call_output', call_id: 'call_obj_1', output: { content: 'structured' } }),
  ri({ type: 'custom_tool_call', status: 'completed', call_id: 'call_patch_1', name: 'apply_patch', input: '*** Begin Patch\n*** Add File: src/new.ts\n+export {};\n*** update file:   src/old file.ts  \n@@\n-a\n+b\n***\tDelete File:\tgone.txt\r\n*** Move File: x\n*** End Patch' }),
  ri({ type: 'custom_tool_call_output', call_id: 'call_patch_1', output: '{"output":"Success","metadata":{"exit_code":0}}' }),
  ri({ type: 'custom_tool_call', status: 'failed', call_id: 'call_patch_2', name: 'apply_patch', input: '' }),
  ri({ type: 'custom_tool_call', call_id: 'call_patch_3', name: 'apply_patch', input: '*** ADD FILE: Upper.md\n' }),
  ri({ type: 'custom_tool_call', status: 'completed', call_id: 'call_custom_1', name: 'js_repl', input: 'console.log(1)' }),
  ri({ type: 'custom_tool_call', status: 'in_progress', call_id: 'call_custom_2', name: 'exec_command', input: { cmd: 'echo custom' } }),
  ri({ type: 'custom_tool_call_output', call_id: 'call_custom_1', output: '1\n2\n3\n4' }),
  ri({ type: 'custom_tool_call_output', call_id: 'call_custom_2', output: '7 lines' }),
  ri({ type: 'custom_tool_call', call_id: 'call_custom_3', input: 'no name' }),
  ri({ type: 'web_search_call', status: 'completed', action: { type: 'search', query: 'go regexp' } }),
  ri({ type: 'web_search_call', status: 'failed', call_id: 'ws_1', query: 'node buffer' }),
  ri({ type: 'web_search_call', call_id: 'ws_2', query: '' }),
  msg('assistant', 'The build fails because of a missing export.', 'commentary'),
  ev({ type: 'agent_message', message: 'The build fails because of a missing export.', phase: 'commentary' }),
  ev({ type: 'agent_message', message: 'A message only as an event.' }),
  msg('assistant', 'A message only as an event.', 'final_answer'),
  ev({ type: 'agent_message', message: '   ' }),
  tokenCount(usage(12000, 8000, 900, 300), usage(4000, 3000, 300, 100)),
  ev({ type: 'task_complete', last_agent_message: 'done' }),
  ev({ type: 'user_message', message: 'Look at these', images: ['data:image/png;base64,AAAA', 5, null], local_images: [`${UP}/ws-1/tab-2/shot one.png`, `${UP}/ws-1/screen.PNG`, `${UP}/ws-1/doc.pdf`, '/etc/passwd.png', 'relative.png', `${UP}/../escape.png`, `${UP}/ws-1/.png`, `${UP}/ws-1/..png`, `${UP}/ws-1/résumé #1?.jpeg`, `${UP}/ws-1/sub/./x.webp`, `${UP}/`, UP, 42] }),
  ev({ type: 'user_message', message: 'Only remote images', images: ['https://example.com/a.gif'] }),
  ev({ type: 'user_message', message: '' }),
  ev({ type: 'user_message', message: '<user_instructions>hidden</user_instructions>' }),
  ev({ type: 'turn_aborted', reason: 'interrupted' }),
  ev({ type: 'TurnAborted' }),
  ev({ type: 'TurnComplete' }),
  ev({ type: 'shutdown_complete' }),
  ev({ type: 'ShutdownComplete' }),
  ev({ type: 'plan_update', plan: [{ task_id: 't1', status: 'in_progress', subject: 'Build' }, { id: 't2', status: 'completed', title: 'Test' }, { status: 'blocked', description: 'Only a description' }, { task_id: null, id: 'fallback-id', status: 'weird' }, { status: 'pending' }, 'skip', null, ['arr']] }),
  ev({ type: 'PlanUpdate', plan: 'not array' }),
  ev({ type: 'entered_review_mode', description: 'Review the diff' }),
  ev({ type: 'EnteredReviewMode' }),
  ev({ type: 'exited_review_mode', outcome: 'approved' }),
  ev({ type: 'ExitedReviewMode', outcome: 'rejected' }),
  ev({ type: 'exited_review_mode', outcome: 'later' }),
  ev({ type: 'exited_review_mode' }),
  ev({ type: 'request_user_input', question: 'Proceed?', call_id: 'ask_1' }),
  ev({ type: 'RequestUserInput', question: 'No call id?' }),
  ev({ type: 'request_user_input' }),
  ev({ type: 'item_completed', item: { type: 'ContextCompaction', id: 'c1' } }),
  ev({ type: 'item_completed', item: { type: 'AgentMessage' } }),
  ev({ type: 'item_completed', item: 'string item' }),
  ev({ type: 'item_completed' }),
  ev({ type: 'context_compacted', before_tokens: 180000, after_tokens: 12000.5 }),
  ev({ type: 'ContextCompacted', before_tokens: '1', after_tokens: 1e400 }),
  rec('compacted', { message: 'summary', replacement_history: [] }),
  ev({ type: 'error', message: 'Something broke' }),
  ev({ type: 'Error' }),
  ev({ type: 'warning', message: 'Careful', retry_status: 'retrying in 2s' }),
  ev({ type: 'stream_error', message: 'stream dropped', codex_error_info: 'ResponseStreamDisconnected', retry_status: 3 }),
  ev({ type: 'StreamError', codex_error_info: { http_status: 500 } }),
  ev({ type: 'guardian_warning', message: 'guard' }),
  ev({ type: 'GuardianWarning', message: '' }),
  ev({ type: 'exec_approval_request', call_id: 'appr_1', command: 'rm -rf build', cwd: '/home/agent/work' }),
  ev({ type: 'ExecApprovalRequest', command: ['rm', 'x'] }),
  ev({ type: 'apply_patch_approval_request', call_id: 'appr_2', patches: [{ path: 'a.ts', status: 'modified' }, { path: 5 }, 'skip', null, ['arr'], { status: 'added' }] }),
  ev({ type: 'request_permissions', call_id: 'appr_3', permissions: ['network', 7, 'disk'] }),
  ev({ type: 'RequestPermissions', patches: 'nope', permissions: 'nope' }),
  ev({ type: 'exec_command_begin', call_id: 'exec_1', command: ['/bin/bash', '-lc', 'cargo test'], cwd: '/home/agent/work' }),
  ev({ type: 'exec_command_delta', call_id: 'exec_1', chunk: 'running 3 tests\n' }),
  ev({ type: 'exec_command_delta', call_id: 'exec_1', data: 'test ok\n' }),
  ev({ type: 'exec_command_delta', call_id: 'exec_1', chunk: '' }),
  ev({ type: 'exec_command_delta', call_id: 'exec_orphan', chunk: 'lost' }),
  ev({ type: 'exec_command_end', call_id: 'exec_1', exit_code: 0, duration: { secs: 2, nanos: 345678901 }, stderr: '', aggregated_output: 'ignored because deltas came' }),
  ev({ type: 'exec_command_begin', call_id: 'exec_2', command: 'make lint' }),
  ev({ type: 'exec_command_end', call_id: 'exec_2', exit_code: 2, duration: 1500.25, stderr: 'lint failed', aggregated_output: 'aggregated text' }),
  ev({ type: 'ExecCommandBegin', call_id: 'exec_3', command: [1, null, ['a', 'b'], true, 'x'] }),
  ev({ type: 'ExecCommandEnd', call_id: 'exec_3', stdout: 'from stdout', duration: [1] }),
  ev({ type: 'exec_command_begin', call_id: 'exec_4', command: '' }),
  ev({ type: 'exec_command_end', call_id: 'exec_4', exit_code: -0, aggregated_output: '', stdout: '' }),
  ev({ type: 'exec_command_end', call_id: 'exec_5', exit_code: 1, command: ['/bin/zsh', '-lc', 'git status', 'extra'], parsed_cmd: [{ type: 'unknown', cmd: 'git status' }], cwd: '/repo', stdout: 'fallback stdout', duration: { secs: 'x', nanos: 5000000 } }),
  ev({ type: 'exec_command_end', call_id: 'exec_6', command: ['/bin/sh', '-lc', 'x', 3], parsed_cmd: ['notobj'], aggregated_output: '' }),
  ev({ type: 'exec_command_end', call_id: 'exec_7', command: 7, parsed_cmd: [{ cmd: 9 }], exit_code: '0' }),
  ev({ type: 'exec_command_end' }),
  ev({ type: 'exec_command_begin' }),
  ev({ type: 'web_search_begin', call_id: 'web_1', query: 'zod record' }),
  ev({ type: 'web_search_end', call_id: 'web_1', query: 'other', results: [{}, {}, {}] }),
  ev({ type: 'WebSearchBegin', call_id: 'web_2' }),
  ev({ type: 'WebSearchEnd', call_id: 'web_2', query: 'ignored', summary: 'sum' }),
  ev({ type: 'web_search_end', call_id: 'web_3', query: 'no begin', results: [] }),
  ev({ type: 'mcp_tool_call_begin', call_id: 'mcp_1', server: 'docs', tool: 'search', arguments: { q: 'timeline   entries', limit: 5, deep: { a: [1, 2] } } }),
  ev({ type: 'mcp_tool_call_end', call_id: 'mcp_1', result: { Ok: { content: [{ type: 'text', text: 'r'.repeat(200) }] } } }),
  ev({ type: 'McpToolCallBegin', call_id: 'mcp_2' }),
  ev({ type: 'McpToolCallEnd', call_id: 'mcp_2', server: 'ignored' }),
  ev({ type: 'mcp_tool_call_end', call_id: 'mcp_3', server: 'srv', tool: 'tl', result: null }),
  ev({ type: 'patch_apply_begin', call_id: 'patch_1', auto_approved: true }),
  ev({ type: 'patch_apply_updated', call_id: 'patch_1', path: 'src/a.ts', status: 'modified' }),
  ev({ type: 'PatchApplyUpdated', call_id: 'patch_1', path: 'src/b.ts' }),
  ev({ type: 'patch_apply_updated', call_id: 'patch_1', path: '' }),
  ev({ type: 'patch_apply_updated', call_id: 'patch_none', path: 'x' }),
  ev({ type: 'patch_apply_end', call_id: 'patch_1', success: true }),
  ev({ type: 'PatchApplyEnd', call_id: 'patch_2', success: false }),
  ev({ type: 'patch_apply_end', call_id: 'patch_3', success: 0 }),
  ev({ type: 'exec_command_begin', call_id: 'reuse', command: 'first' }),
  ev({ type: 'web_search_begin', call_id: 'left_web', query: 'q' }),
  ev({ type: 'web_search_begin', call_id: 'reuse', query: 'second' }),
  ev({ type: 'mcp_tool_call_begin', call_id: 'left_mcp', server: 's', tool: 't' }),
  ev({ type: 'patch_apply_begin', call_id: 'left_patch' }),
  ev({ type: 'patch_apply_updated', call_id: 'left_patch', path: 'p.txt', status: 'added' }),
  ev({ type: 'exec_command_begin', call_id: 'left_exec', command: 'sleep 100', cwd: '/w' }),
  ev({ type: 'exec_command_delta', call_id: 'left_exec', chunk: 'partial out' }),
  ev({ type: 'unknown_event', foo: 1 }),
  ri({ type: 'unknown_item' }),
  rec('world_state', { anything: true }),
  rec('mystery', {}),
  JSON.stringify({ timestamp: '2026-03-01T13:00:00+03:00', type: 'event_msg', payload: { type: 'turn_aborted' } }),
  JSON.stringify({ type: 'event_msg', payload: { type: 'TurnComplete' } }),
  JSON.stringify({ timestamp: 'nope', type: 'event_msg', payload: { type: 'task_complete' } }),
  JSON.stringify({ timestamp: '', type: 'event_msg', payload: { type: 'task_complete' } }),
  JSON.stringify({ timestamp: '2026-03-01', type: 'event_msg', payload: { type: 'task_complete' } }),
  JSON.stringify({ timestamp: '2026-03-01T10:00:00.123456Z', type: 'event_msg', payload: { type: 'task_complete' } }),
  JSON.stringify({ timestamp: '2026-03-01T10:00:00.1+0530', type: 'event_msg', payload: { type: 'task_complete' } }),
  JSON.stringify({ timestamp: ts(), type: 'event_msg' }),
  turnContext('gpt-5.4', 'medium'),
  msg('user', 'Second turn ✓'),
  msg('user', 'Second turn ✓'),
  ev({ type: 'user_message', message: 'Second turn ✓' }),
  ev({ type: 'user_message', message: 'Second turn ✓' }),
  tokenCount(usage(30000, 20000, 2000, 700), usage(9000, 8000, 500, 200)),
];
write('basic.jsonl', [lines(basic)]);

const cyr = 'Привет, мир! Это проверка многобайтового текста ';
const emojiAt99 = 'a'.repeat(98) + '😀' + ' tail';
const malformed = [
  '',
  '   ',
  '\t',
  'not json',
  '{"type": "event_msg", "payload": {"type": "task_complete"}',
  '[1,2,3]',
  '42',
  'null',
  '"string"',
  '{}',
  '{"type": 5}',
  '{"type": "event_msg", "timestamp": 5, "payload": {"type": "task_complete"}}',
  '{"type": "event_msg", "timestamp": null, "payload": {"type": "task_complete"}}',
  '{"type": "event_msg", "payload": null}',
  '{"type": "event_msg", "payload": [1]}',
  '{"type": "event_msg", "payload": "str"}',
  '{"type":"event_msg","payload":{"__proto__":{"type":"task_complete"},"type":"turn_aborted"}}',
  '{"type":"event_msg","payload":{"type":"task_complete"},"type":"response_item"}',
  '  {"type":"event_msg","payload":{"type":"task_complete"}}  ',
  '﻿{"type":"event_msg","payload":{"type":"task_complete"}}',
  ' ',
  rec('event_msg', { type: 'user_message', message: cyr.repeat(3) }),
  rec('event_msg', { type: 'agent_message', message: '日本語のテキスト 行区切り nbsp' }),
  msg('user', emojiAt99),
  ev({ type: 'user_message', message: emojiAt99 }),
  ri({ type: 'function_call', name: 'emoji', arguments: JSON.stringify({ q: 'x'.repeat(57) + '😀😀😀' }), call_id: 'c_emoji' }),
  ri({ type: 'function_call_output', call_id: 'c_emoji', output: 'y'.repeat(98) + '😀z' }),
  ev({ type: 'mcp_tool_call_begin', call_id: 'm_emoji', server: 's', tool: 't', arguments: { k: 'é'.repeat(70) + '😀' } }),
  ev({ type: 'mcp_tool_call_end', call_id: 'm_emoji', result: '\u0001\u001f "quoted" \\ back' }),
  '{"timestamp":"2026-03-01T10:00:00Z","type":"event_msg","payload":{"type":"agent_message","message":"lone \\ud83d high, pair \\ud83d\\ude00"}}',
  rec('event_msg', { type: 'agent_message', message: 'crlf line' }) + '\r',
  rec('event_msg', { type: 'turn_aborted' }) + '\r',
];
const invalid = Buffer.from([0x7b, 0x22, 0x74, 0x79, 0x70, 0x65, 0x22, 0x3a, 0x22, 0x65, 0x76, 0x65, 0x6e, 0x74, 0x5f, 0x6d, 0x73, 0x67, 0x22, 0x2c, 0x22, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x22, 0x3a, 0x7b, 0x22, 0x74, 0x79, 0x70, 0x65, 0x22, 0x3a, 0x22, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x5f, 0x6d, 0x73, 0x67, 0x22, 0x2c, 0x22, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x3a, 0x22, 0x62, 0x61, 0x64, 0x20, 0xe2, 0x82, 0x20, 0xff, 0xed, 0xa0, 0x80, 0xf0, 0x9f, 0x98, 0x20, 0x6f, 0x6b, 0x22, 0x7d, 0x7d, 0x0a]);
write('malformed.jsonl', [
  lines(malformed),
  invalid,
  Buffer.from([0xc3, 0xa9, 0x0a]),
  lines([ev({ type: 'user_message', message: 'after the invalid bytes — ещё' }), ev({ type: 'task_complete' })]),
  rec('event_msg', { type: 'agent_message', message: 'partial last line that never ends' }).slice(0, 70),
]);

write('throws.jsonl', [lines([
  meta(),
  ev({ type: 'user_message', message: 'before the throw' }),
  '{"timestamp":"2026-03-01T10:00:00Z","type":"event_msg","payload":{"type":{"toString":1}}}',
  ev({ type: 'task_complete' }),
])]);
write('throws-exec.jsonl', [lines([
  ev({ type: 'user_message', message: 'array command with an object' }),
  ev({ type: 'exec_command_begin', call_id: 'e', command: ['ok', { toString: 'no' }] }),
  ev({ type: 'exec_command_begin', call_id: 'f', command: ['ok', { valueOf: 'fine' }] }),
  ev({ type: 'exec_command_end', call_id: 'f', exit_code: 0 }),
])]);
write('payload-type-array.jsonl', [lines([
  ev({ type: 'agent_message', message: 'same text' }),
  rec('event_msg', { type: ['task_started'] }),
  msg('assistant', 'same text'),
  ev({ type: 'agent_message', message: 'other' }),
  rec('event_msg', { type: { valueOf: 1 } }),
  msg('assistant', 'other'),
])]);

// Above the 256 000-byte chunk: a stdout that passes the 1 MiB limit, a line longer than a chunk, and many short lines.
const big = [meta('0199a000-0000-7000-8000-00000000b16b'), turnContext('gpt-5.1-codex-max', 'xhigh'), tokenCount(usage(1000, 0, 10, 0), usage(1000, 0, 10, 0)), msg('user', 'Make a big file')];
big.push(ev({ type: 'exec_command_begin', call_id: 'big_exec', command: 'yes' }));
// Four deltas of 209 003 units, then one whose cut at 1 048 576 units falls between the halves of its emoji.
for (let i = 0; i < 4; i++) big.push(ev({ type: 'exec_command_delta', call_id: 'big_exec', chunk: `${i}`.repeat(209_000) + 'é😀' }));
big.push(ev({ type: 'exec_command_delta', call_id: 'big_exec', chunk: '4'.repeat(212_562) + 'é😀' }));
big.push(ev({ type: 'exec_command_delta', call_id: 'big_exec', chunk: 'after the limit' }));
big.push(ev({ type: 'exec_command_end', call_id: 'big_exec', exit_code: 0 }));
big.push(ri({ type: 'function_call_output', call_id: 'nobody', output: 'L'.repeat(300_000) }));
for (let i = 0; i < 1500; i++) {
  big.push(i % 3 === 0 ? msg('user', `question ${i}`) : i % 3 === 1 ? msg('assistant', `answer ${i} ${'·'.repeat(i % 40)}`) : ev({ type: 'task_complete' }));
}
big.push(msg('assistant', 'last words'));
big.push(ri({ type: 'function_call_output', call_id: 'tail', output: 'T'.repeat(270_000) }));
write('big.jsonl', [lines(big)]);

// Session statistics variants.
const stats = {
  'stats-long-context.jsonl': [meta(), turnContext('gpt-5.4', 'high'), tokenCount(usage(500000, 100000, 20000, 3000), usage(250000, 0, 1000, 0), 400000)],
  'stats-dated-model.jsonl': [meta(), turnContext('OpenAI/GPT-5.4-mini-2026-01-15 ', 'low'), tokenCount(usage(1234567, 234567, 8910, 11), usage(7, 3, 2, 1), 128000)],
  'stats-unknown-model.jsonl': [meta(), turnContext('o3-pro'), tokenCount(usage(10, 5, 1, 0), null, 0)],
  'stats-no-tokens.jsonl': [meta(), turnContext('gpt-5')],
  'stats-no-meta.jsonl': [turnContext('gpt-5'), tokenCount(usage(10, 5, 1, 0), usage(10, 5, 1, 0))],
  'stats-meta-top-ts.jsonl': [JSON.stringify({ timestamp: '2026-03-01T09:00:00Z', type: 'session_meta', payload: { id: 'top-ts' } }), tokenCount(usage(10, 20, 1, 0), usage(10, 5, 1, 0), 1000)],
  'stats-meta-no-ts.jsonl': [JSON.stringify({ type: 'session_meta', payload: { id: 'no-ts' } }), tokenCount(usage(10, 5, 1, 0), usage(10, 5, 1, 0))],
  'stats-meta-number-id.jsonl': [JSON.stringify({ type: 'session_meta', payload: { id: 12345, timestamp: 2026 } }), turnContext('gpt-4o-mini-20240718', 'none'), tokenCount(usage(10, 5, 1, 0), usage(10, 5, 1, 0), 99)],
  'stats-weird-types.jsonl': [meta('weird'), turnContext('gpt-5.5', 5), ev({ type: 'token_count', info: { total_token_usage: { input_tokens: '300000', cached_input_tokens: [100], output_tokens: true, reasoning_output_tokens: 'r', total_tokens: null }, last_token_usage: { total_tokens: ' 0x30D40 ' }, model_context_window: '250000' } })],
  'stats-object-tostring.jsonl': [meta('tostring'), turnContext('gpt-5'), ev({ type: 'token_count', info: { total_token_usage: { input_tokens: { toString: 1 } }, model_context_window: 1000 } })],
  'stats-object-valueof.jsonl': [meta('valueof'), turnContext('gpt-5'), ev({ type: 'token_count', info: { total_token_usage: { input_tokens: { valueOf: 1 }, output_tokens: 5 }, last_token_usage: { total_tokens: [1, [2]] }, model_context_window: 1000 } })],
  'stats-last-only.jsonl': [meta('last'), ev({ type: 'token_count', info: { total_token_usage: null, last_token_usage: usage(900, 100, 50, 5), model_context_window: 200000 } }), turnContext('', 'high'), turnContext('gpt-5.2-codex')],
  'stats-info-string.jsonl': [meta('s'), turnContext('gpt-5'), ev({ type: 'token_count', info: 'not an object' }), ev({ type: 'token_count', info: null })],
  'stats-crlf.jsonl': [meta('crlf') + '\r', turnContext('gpt-5-nano') + '\r', tokenCount(usage(100, 0, 100, 0), usage(100, 0, 100, 0), 400) + '\r\r' + 'garbage', '   '],
  'stats-blank-first.jsonl': ['', '   ', meta('blank-first'), tokenCount(usage(1, 0, 1, 0), usage(1, 0, 1, 0), 3)],
  'stats-bad-first.jsonl': ['{oops', meta('bad-first')],
  'stats-null-first.jsonl': ['null', meta('null-first')],
  'stats-meta-ts-object.jsonl': [JSON.stringify({ type: 'session_meta', payload: { id: 'o', timestamp: { toString: 1 } } })],
  'stats-meta-ts-array.jsonl': [JSON.stringify({ type: 'session_meta', payload: { id: 'a', timestamp: ['2026-02-03T04:05:06Z'] } }), tokenCount(usage(1, 0, 1, 0), usage(1, 0, 1, 0), 3)],
  'stats-infinity.jsonl': [meta('inf'), turnContext('gpt-5'), '{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1e400,"output_tokens":1},"last_token_usage":{"total_tokens":1e400},"model_context_window":1}}}'],
};
for (const [name, ls] of Object.entries(stats)) write(name, [lines(ls)]);
write('empty.jsonl', []);
write('one-partial.jsonl', [rec('event_msg', { type: 'agent_message', message: 'no newline at all' })]);
