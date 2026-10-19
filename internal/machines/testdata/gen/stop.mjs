// Runs the previous runtime's own process stopper (the load panel's stop button) on a /proc of its own, and its
// request reader on generated bodies, and writes what each answered to ../stop.json. The Go test lays out the same
// /proc, plays the same signals and must answer alike.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_MODULE_PREFIX=<its module word> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs stop.mjs
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const P = process.env.OLD_RUNTIME_MODULE_PREFIX;
if (!P) throw new Error('OLD_RUNTIME_MODULE_PREFIX is required');
const { stopProcess, parseStopBody } = await import(`@/lib/${P}-machine-stop`);
const { RELEASE_ROOT } = await import(`@/lib/${P}-machine-load`);
const NEUTRAL_ROOT = '/opt/acme-ai/';
const here = path.dirname(new URL(import.meta.url).pathname);

// --- request bodies -----------------------------------------------------------------------------------------------
const bodies = ['{"pid":1603,"starttime":"2603"}', '{"pid":1603,"starttime":2603}', '{"pid":1603.5,"starttime":"1"}', '{"pid":1,"starttime":"1"}',
  '{"pid":2,"starttime":"0"}', '{"pid":4194304,"starttime":"0"}', '{"pid":4194305,"starttime":"0"}', '{"pid":"1603","starttime":"1"}',
  '{"pid":1603,"starttime":"01"}', '{"pid":1603,"starttime":"-1"}', '{"pid":1603,"starttime":-0}', '{"pid":1603,"starttime":1.5}',
  '{"pid":1603,"starttime":9007199254740991}', '{"pid":1603,"starttime":9007199254740992}', '{"pid":1603,"starttime":1e3}', '{"pid":1603}',
  '{"pid":1603,"starttime":null}', '[]', 'null', '"x"', '5', '{}', '{"pid":-5,"starttime":"1"}', '{"pid":1e3,"starttime":" 1"}',
  '{"pid":3000,"starttime":"123456789012345678901234567890"}', '{"pid":2.0,"starttime":"7"}', '{"pid":1603,"starttime":true}'];
const parsed = bodies.map((b) => [b, parseStopBody(JSON.parse(b))]);

// --- the stopper on a /proc of its own -------------------------------------------------------------------------------
const UID = 1000;
const REL = RELEASE_ROOT.replace(/\/$/, '');
const proc = (pid, { comm, ppid, uid = UID, euid = uid, start = 1000 + pid, state = 'S', args = comm, cwd = '/home/agent', exe = '/usr/bin/x', cgroup = '0::/system.slice/runtime.service' }) => ({
  [`${pid}/stat`]: `${pid} (${comm}) ${state} ${ppid} 1 1 0 -1 4194560 100 0 0 0 5 5 0 0 20 0 1 0 ${start} 100000 50 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n`,
  [`${pid}/status`]: `Name:\t${comm}\nUid:\t${uid}\t${euid}\t${euid}\t${euid}\n`,
  [`${pid}/cmdline`]: args.split(' ').join('\0') + '\0',
  [`${pid}/cgroup`]: cgroup,
  [`${pid}/cwd`]: { link: cwd },
  [`${pid}/exe`]: { link: exe },
});
const tree = {
  ...proc(900, { comm: 'systemd', ppid: 1, uid: 0 }),
  ...proc(950, { comm: 'bash', ppid: 900, args: '-bash' }),
  ...proc(1000, { comm: 'node', ppid: 950, args: `node ${REL}/current/server.js`, exe: '/usr/bin/node' }),
  ...proc(1100, { comm: 'chrome', ppid: 1000, args: 'chrome --headless' }),
  ...proc(1500, { comm: 'tmux: server', ppid: 1, args: 'tmux -L runtime new-session', exe: `${REL}/bin/tmux` }),
  ...proc(1501, { comm: 'bash', ppid: 1500, args: '-bash' }),
  ...proc(1502, { comm: 'zsh', ppid: 1500, args: 'zsh' }),
  ...proc(1600, { comm: 'claude', ppid: 1501, args: 'claude --dangerously-skip-permissions' }),
  ...proc(1601, { comm: 'node', ppid: 1600, args: 'node /home/agent/mcp/server.js' }),
  ...proc(1602, { comm: 'bash', ppid: 1600, args: 'bash -c npm test' }),
  ...proc(1603, { comm: 'node', ppid: 1602, args: 'node vite --port 5173', cwd: '/home/agent/app' }),
  ...proc(1604, { comm: 'node', ppid: 1600, args: 'node vite', cwd: '/home/agent/app' }),
  ...proc(1700, { comm: 'node', ppid: 1502, args: 'node next dev', cwd: '/home/agent/site' }),
  ...proc(1701, { comm: 'python3', ppid: 1502, args: 'python3 -m http.server 8000' }),
  ...proc(1702, { comm: 'go', ppid: 1502, args: 'go run .', start: 99 }),
  ...proc(1800, { comm: 'sudo', ppid: 1502, euid: 0, args: 'sudo ls' }),
  ...proc(1801, { comm: 'ls', ppid: 1800, args: 'ls -R /' }),
  ...proc(1900, { comm: 'other', ppid: 1, uid: 1001 }),
  ...proc(2000, { comm: 'netbird', ppid: 900, cgroup: '0::/system.slice/netbird.service' }),
  ...proc(2100, { comm: 'agent', ppid: 900, cgroup: '0::/system.slice/other-agent.service', args: '/opt/x/agent' }),
  ...proc(2101, { comm: 'helper', ppid: 2100, cgroup: '0::/user.slice/u.scope' }),
  ...proc(2102, { comm: 'helper2', ppid: 2101, cgroup: '0::/user.slice/u.scope' }),
  ...proc(2200, { comm: 'zombie', ppid: 1502, state: 'Z' }),
  ...proc(2300, { comm: 'we(ird) (name', ppid: 1502, args: '' }),
  ...proc(2400, { comm: 'node', ppid: 1, args: 'node /srv/app.js', cwd: `${REL}/current/.next/standalone` }),
  ...proc(2500, { comm: 'nginx', ppid: 1502, args: 'nginx -g daemon off;' }),
  ...proc(2600, { comm: 'tmux: server', ppid: 1, args: 'tmux', cgroup: '0::/system.slice/tmux-host.service' }),
  ...proc(2601, { comm: 'node', ppid: 2600, args: 'node worker.js', cgroup: '0::/user.slice/u.scope' }),
  ...proc(2700, { comm: 'node', ppid: 1502, args: 'node tool.js' }),
  ...proc(2701, { comm: 'bash', ppid: 2700, args: 'bash' }),
};
const listed = [1603, 1604, 1700, 1701, 1702, 1801, 2101, 2102, 2300, 2400, 2500, 2601, 2701, 1100, 1600]
  .map((pid) => ({ pid, starttime: 1000 + pid, ...(pid === 1603 || pid === 1700 || pid === 1604 ? { port: 5000 + pid } : {}) }))
  .concat([{ pid: 1702, starttime: 1 }]);

// Each case: the request, then what the signals do. term/kill: 'sent' | 'gone' | 'denied'; after: the polls until the
// process is gone, a zombie or a new process (null: it stays).
const cases = [
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: 3, end: 'gone' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: 1, end: 'zombie' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: 7, end: 'reused' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: null, kill: 'sent' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: null, kill: 'gone' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'sent', after: null, kill: 'denied' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'gone' }],
  [{ pid: 1603, starttime: '2603' }, { term: 'denied' }],
  [{ pid: 1603, starttime: '2604' }, {}],
  [{ pid: 4000, starttime: '1' }, {}],
  [{ pid: 2200, starttime: '3200' }, {}],
  [{ pid: 1900, starttime: '2900' }, {}],
  [{ pid: 1800, starttime: '2800' }, {}],
  [{ pid: 1801, starttime: '2801' }, { term: 'sent', after: 0, end: 'gone' }],
  [{ pid: 1000, starttime: '2000' }, {}],
  [{ pid: 950, starttime: '1950' }, {}],
  [{ pid: 900, starttime: '1900' }, {}],
  [{ pid: 1100, starttime: '2100' }, {}],
  [{ pid: 2000, starttime: '3000' }, {}],
  [{ pid: 2101, starttime: '3101' }, {}],
  [{ pid: 2102, starttime: '3102' }, {}],
  [{ pid: 2400, starttime: '3400' }, {}],
  [{ pid: 1500, starttime: '2500' }, {}],
  [{ pid: 1501, starttime: '2501' }, {}],
  [{ pid: 1600, starttime: '2600' }, {}],
  [{ pid: 1601, starttime: '2601' }, {}],
  [{ pid: 1602, starttime: '2602' }, {}],
  [{ pid: 1604, starttime: '2604' }, { term: 'sent', after: 2, end: 'gone' }],
  [{ pid: 1700, starttime: '2700' }, { term: 'sent', after: 49, end: 'gone' }],
  [{ pid: 1700, starttime: '2700' }, { term: 'sent', after: 50, end: 'gone', kill: 'gone' }],
  [{ pid: 1701, starttime: '2701' }, { term: 'sent', after: 0, end: 'gone' }],
  [{ pid: 1702, starttime: '1099' }, {}],
  [{ pid: 2300, starttime: '3300' }, { term: 'sent', after: 0, end: 'gone' }],
  [{ pid: 2500, starttime: '3500' }, {}],
  [{ pid: 2601, starttime: '3601' }, { term: 'sent', after: 0, end: 'gone' }],
  [{ pid: 2700, starttime: '3700' }, {}],
  [{ pid: 2701, starttime: '3701' }, { term: 'sent', after: 0, end: 'gone' }],
];

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'stop-golden-')));
const materialize = (dir) => {
  fs.rmSync(dir, { recursive: true, force: true });
  for (const [name, content] of Object.entries(tree)) {
    const file = path.join(dir, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    if (typeof content === 'object') fs.symlinkSync(content.link, file);
    else fs.writeFileSync(file, content);
  }
};
const procDir = path.join(root, 'proc');
const results = [];
for (const [request, plan] of cases) {
  materialize(procDir);
  const calls = [];
  let polls = 0;
  let termed = false;
  const end = (pid) => {
    const stat = path.join(procDir, String(pid), 'stat');
    if (plan.end === 'gone') fs.rmSync(stat);
    else if (plan.end === 'zombie') fs.writeFileSync(stat, fs.readFileSync(stat, 'utf8').replace(/\) S /, ') Z '));
    else fs.writeFileSync(stat, fs.readFileSync(stat, 'utf8').replace(/ 0 (\d+) 100000/, ' 0 7$1 100000'));
  };
  const deps = {
    uid: UID,
    runtimePid: 1000,
    read: async (pid, file) => { try { return fs.readFileSync(path.join(procDir, String(pid), file), 'utf8'); } catch { return null; } },
    exe: async (pid) => { try { return fs.readlinkSync(path.join(procDir, String(pid), 'exe')); } catch { return null; } },
    cwd: async (pid) => { try { return fs.readlinkSync(path.join(procDir, String(pid), 'cwd')); } catch { return null; } },
    kill: (pid, signal) => {
      calls.push(signal);
      const outcome = signal === 'SIGTERM' ? plan.term : plan.kill ?? 'sent';
      if (signal === 'SIGTERM' && outcome === 'sent') {
        termed = true;
        if (plan.after === 0) end(pid);
      }
      return outcome;
    },
    panePids: async () => [1501, 1502],
    listed: async () => listed,
    sleep: async () => {
      polls += 1;
      if (termed && plan.after !== null && polls === plan.after) end(request.pid);
    },
  };
  const res = await stopProcess(deps, request);
  results.push({ request, plan, calls, polls, result: res });
}
fs.rmSync(root, { recursive: true, force: true });

const neutral = (v) => JSON.parse(JSON.stringify(v).split(RELEASE_ROOT).join(NEUTRAL_ROOT).split(REL).join(NEUTRAL_ROOT.replace(/\/$/, '')));
fs.writeFileSync(path.join(here, '..', 'stop.json'), JSON.stringify(neutral({ uid: UID, releaseRoot: RELEASE_ROOT, tree, listed, bodies: parsed, cases: results }), null, 1) + '\n');
console.log(`${results.length} stops, ${parsed.length} bodies → ${path.join(here, '..', 'stop.json')}`);
