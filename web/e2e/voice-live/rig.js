// The voice rig: a live talk without a person. Injected into a signed-in harnessly page (a browser
// tool's evaluate, or Playwright's addInitScript), it puts a mixer in place of the microphone; window.__rig.say('theme')
// plays e2e/voice-live/out/theme.wav into it, as if the person said it, and window.__rig.state() reads what the page
// did: the route, the theme, the sidebar, the voice's action lines. The talk itself is the real one (the realtime
// model, the router), so this checks what unit tests cannot: the whole way from a spoken phrase to the page.
(() => {
  if (window.__rig) return 'already';
  const base = window.__rigBase || 'http://127.0.0.1:18911/';
  const ctx = new AudioContext({ sampleRate: 48000 });
  const dest = ctx.createMediaStreamDestination();
  // A faint hum keeps the track live between phrases, as a real room does.
  const hum = ctx.createOscillator();
  const quiet = ctx.createGain();
  quiet.gain.value = 0.0005;
  hum.connect(quiet).connect(dest);
  hum.start();
  const own = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
  navigator.mediaDevices.getUserMedia = async (c) => {
    if (c && c.audio && !c.video) {
      await ctx.resume();
      return dest.stream;
    }
    return own(c);
  };
  const log = [];
  window.__rig = {
    log,
    async say(name, gain = 1) {
      await ctx.resume();
      const data = await (await fetch(`${base}${name}.wav`)).arrayBuffer();
      const buf = await ctx.decodeAudioData(data);
      const src = ctx.createBufferSource();
      const g = ctx.createGain();
      g.gain.value = gain;
      src.buffer = buf;
      src.connect(g).connect(dest);
      src.start();
      log.push({ said: name, at: Date.now() });
      return buf.duration;
    },
    state() {
      const q = (s) => [...document.querySelectorAll(s)].map((e) => e.textContent.trim());
      return {
        path: location.pathname,
        theme: document.documentElement.dataset.theme || document.documentElement.className,
        acts: q('.hx-vacts__row'),
        voice: q('.hx-voice-full__state, .hx-voice-bar__t'),
        line: q('.hx-voice-full__line'),
        ctx: ctx.state,
      };
    },
  };
  return 'ok';
})();
