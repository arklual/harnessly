package design

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The recording part of the board script, run in node against a stand-in page: a click is noted with its element and
// label but never a typed value, events leave in batches, a guest records nothing. Both language twins carry the
// same code.
const timelineClientDriver = `
const handlers = {};
const calls = [];
const fakeEl = (tag, extra) => Object.assign({ nodeType: 1, tagName: tag.toUpperCase(), id: '', className: '', parentElement: null,
  getAttribute: () => null, textContent: '', closest() { return this; }, isContentEditable: false }, extra || {});
function run(guest) {
  calls.length = 0;
  for (const k of Object.keys(handlers)) delete handlers[k];
  const doc = { hidden: false, visibilityState: 'visible', scrollingElement: { scrollTop: 0, scrollHeight: 2000 }, documentElement: {},
    addEventListener: (n, f) => { (handlers['d:' + n] = handlers['d:' + n] || []).push(f); } };
  const win = { devicePixelRatio: 2, addEventListener: (n, f) => { (handlers['w:' + n] = handlers['w:' + n] || []).push(f); } };
  const stubs = { document: doc, window: win, location: { pathname: '/a/x/p/Main.dc.html', search: '?v=Desktop', href: 'http://h/a/x/p/Main.dc.html', origin: 'http://h' },
    history: {}, innerWidth: 1440, innerHeight: 900, console: { error() {}, warn() {} },
    performance: { now: () => 1000, timeOrigin: 0 }, PerformanceObserver: function () { throw new Error('no'); },
    requestAnimationFrame: () => 0, requestIdleCallback: undefined, XMLHttpRequest: undefined,
    fetch: (u, o) => { calls.push(u); return Promise.resolve({ ok: true, status: 200 }); }, URL };
  stubs.window.fetch = stubs.fetch;
  const body = BLOCK;
  return new Function('guest', 'id', ...Object.keys(stubs), 'var noteTweak = function () {};' + body + ';return noteTweak;')(guest, 'x', ...Object.values(stubs));
}
function events() { return calls.flatMap((u) => JSON.parse(decodeURIComponent(u.split('&e=')[1]))); }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
(async () => {
  run(true);
  if (calls.length || Object.keys(handlers).length) throw new Error('a guest recorded');
  run(false);
  const password = fakeEl('input', { type: 'password', value: 'hunter2', name: 'pass', getAttribute: (n) => (n === 'placeholder' ? 'Password' : null) });
  handlers['d:click'][0]({ target: password, clientX: 10.4, clientY: 20.6 });
  const button = fakeEl('button', { textContent: '  Buy   now  ', className: 'cta big extra' });
  for (let i = 0; i < 24; i += 1) handlers['d:click'][0]({ target: button, clientX: i, clientY: 1 });
  await sleep(30);
  const first = events();
  if (first.length < 20 || calls.some((u) => u.length > 6500)) throw new Error('events did not leave in batches: ' + calls.length + ' calls, ' + first.length + ' events');
  handlers['w:pagehide'][0]();
  await sleep(10);
  const all = events();
  const clicks = all.filter((e) => e.k === 'click');
  if (all[0].k !== 'nav' || all[0].u !== 'Main.dc.html?v=Desktop' || all[0].w !== 1440) throw new Error('no navigation first: ' + JSON.stringify(all[0]));
  if (clicks.length !== 25) throw new Error('clicks = ' + clicks.length);
  if (clicks[0].tx !== 'Password' || JSON.stringify(all).includes('hunter2')) throw new Error('a typed value leaked');
  if (clicks[1].tx !== 'Buy now' || clicks[1].sel !== 'button.cta.big') throw new Error('click = ' + JSON.stringify(clicks[1]));
  const noteTweak = run(false);
  noteTweak('theme', 'dark');
  handlers['w:pagehide'][0]();
  await sleep(10);
  const tweak = events().find((e) => e.k === 'tweak');
  if (!tweak || tweak.n !== 'theme' || tweak.v !== 'dark') throw new Error('tweak = ' + JSON.stringify(tweak));
  console.log('ok');
})().catch((e) => { console.error(e.message); process.exit(1); });
`

func TestTimelineClient(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on this machine")
	}
	for _, file := range []string{"texts/client.js", "texts/client.ru.js"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		start := strings.Index(src, "  if (!guest) (function () {\n    var FLUSH_MS")
		end := strings.Index(src, "\n  var current = null;")
		if start < 0 || end < start {
			t.Fatalf("%s: the timeline block is not where the test looks", file)
		}
		block, _ := json.Marshal(src[start:end])
		script := strings.Replace(timelineClientDriver, "BLOCK", string(block), 1)
		cmd := exec.Command(node, "-e", script)
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("%s: %v\n%s", file, err, out)
		}
	}
}
