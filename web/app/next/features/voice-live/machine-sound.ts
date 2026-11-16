// The sound of a call over the machine: for networks that carry no UDP to the voice's media servers, or
// lose its answers on the way back (a VPN client that tunnels TCP well and UDP badly), the machine holds the peer
// connection and the page sends and gets Opus frames over one WebSocket on its usual HTTPS way to the machine
// (internal/voice/realtime/relay.go). The page encodes the microphone and decodes the voice itself (WebCodecs), so the
// machine forwards frames as they are.

/** One Opus frame: 20 ms at 48 kHz. */
const RATE = 48_000;
const FRAME = 960;
/** How far ahead of now the voice's sound is played: enough for the network's unevenness, short enough to talk. */
const LEAD_S = 0.08;
/** More queued than this and the oldest is dropped: a network that stalled does not leave the voice lagging. */
const BEHIND_S = 0.5;
/** A socket this full skips the microphone's frames instead of queueing a delay. */
const SEND_BUFFERED = 32 << 10;

export const MACHINE_STATE = {
  connecting: 'connecting',
  connected: 'connected',
  failed: 'failed',
  closed: 'closed',
} as const;
export type TMachineState = (typeof MACHINE_STATE)[keyof typeof MACHINE_STATE];

export interface IMachineSound {
  /** The voice's sound, for the page's audio element and loudness meter. */
  stream: MediaStream;
  /** Sends an event to the call's data channel (the key route's cancels). */
  send: (event: string) => void;
  open: () => boolean;
  /** What the socket brought so far, for the journal: the machine's last state, text and sound messages counted. */
  seen: () => string;
  /** Lets the sound run again after the browser held it back. */
  resume: () => void;
  close: () => void;
}

/** Whether this browser can carry a call's sound over the machine. */
export const machineSoundWorks = (): boolean =>
  typeof AudioEncoder !== 'undefined' &&
  typeof AudioDecoder !== 'undefined' &&
  typeof AudioData !== 'undefined' &&
  typeof WebSocket !== 'undefined';

/** Cuts a stream of samples into whole frames; the rest waits for the next samples. */
export const framer = (size: number, onFrame: (frame: Float32Array<ArrayBuffer>) => void) => {
  let buf = new Float32Array(size);
  let at = 0;
  return (samples: Float32Array) => {
    let i = 0;
    while (i < samples.length) {
      const n = Math.min(size - at, samples.length - i);
      buf.set(samples.subarray(i, i + n), at);
      at += n;
      i += n;
      if (at === size) {
        onFrame(buf);
        buf = new Float32Array(size);
        at = 0;
      }
    }
  };
};

/** When the next piece of the voice's sound starts: right after the last one, or a little ahead of now after a gap;
 *  null drops a piece that would play too late. */
export const nextStart = (queued: number, now: number): number | null => {
  if (queued < now + 0.01) return now + LEAD_S;
  if (queued > now + BEHIND_S) return null;
  return queued;
};

const TAP = `class Tap extends AudioWorkletProcessor {
  process(inputs) { const ch = inputs[0] && inputs[0][0]; if (ch) this.port.postMessage(ch.slice(0)); return true; }
}
registerProcessor('hx-mic-tap', Tap);`;

/** Calls on with the microphone's samples as they come: an AudioWorklet, or the old ScriptProcessor where none loads. */
const tapMic = async (ctx: AudioContext, mic: MediaStream, on: (s: Float32Array) => void) => {
  const src = ctx.createMediaStreamSource(mic);
  const sink = ctx.createGain();
  sink.gain.value = 0;
  sink.connect(ctx.destination);
  try {
    const url = URL.createObjectURL(new Blob([TAP], { type: 'text/javascript' }));
    try {
      await ctx.audioWorklet.addModule(url);
    } finally {
      URL.revokeObjectURL(url);
    }
    const node = new AudioWorkletNode(ctx, 'hx-mic-tap', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      channelCount: 1,
    });
    node.port.onmessage = (e: MessageEvent<Float32Array>) => on(e.data);
    src.connect(node).connect(sink);
    return () => {
      node.port.onmessage = null;
      src.disconnect();
      node.disconnect();
    };
  } catch {
    const node = ctx.createScriptProcessor(1024, 1, 1);
    node.onaudioprocess = (e) => on(e.inputBuffer.getChannelData(0).slice(0));
    src.connect(node).connect(sink);
    return () => {
      node.onaudioprocess = null;
      src.disconnect();
      node.disconnect();
    };
  }
};

/**
 * Joins a call's sound over the machine at the socket url: the microphone goes out as Opus frames, the voice's frames
 * are played into the returned stream. on.state tells how the machine's own connection is, on.event the data
 * channel's messages.
 */
export const joinMachineSound = async (
  url: string,
  mic: MediaStream,
  on: { state: (s: TMachineState) => void; event: (data: string) => void },
): Promise<IMachineSound> => {
  const ctx = new AudioContext({ sampleRate: RATE, latencyHint: 'interactive' });
  const out = ctx.createGain();
  const dest = ctx.createMediaStreamDestination();
  out.connect(dest);
  let closed = false;
  let untap = () => undefined as void;
  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';

  let queued = 0;
  const playing = new Set<AudioBufferSourceNode>();
  const decoder = new AudioDecoder({
    output: (d) => {
      try {
        const n = d.numberOfFrames;
        const pcm = new Float32Array(n);
        d.copyTo(pcm, { planeIndex: 0, format: 'f32-planar' });
        const at = nextStart(queued, ctx.currentTime);
        if (at === null) return;
        const buf = ctx.createBuffer(1, n, d.sampleRate);
        buf.copyToChannel(pcm, 0);
        const src = ctx.createBufferSource();
        src.buffer = buf;
        src.connect(out);
        src.onended = () => playing.delete(src);
        playing.add(src);
        src.start(at);
        queued = at + buf.duration;
      } finally {
        d.close();
      }
    },
    error: () => undefined,
  });
  decoder.configure({ codec: 'opus', sampleRate: RATE, numberOfChannels: 1 });
  let inAt = 0;

  let outAt = 0;
  const encoder = new AudioEncoder({
    output: (chunk) => {
      if (ws.readyState !== WebSocket.OPEN || ws.bufferedAmount > SEND_BUFFERED) return;
      const b = new Uint8Array(chunk.byteLength);
      chunk.copyTo(b);
      ws.send(b);
    },
    error: () => undefined,
  });
  encoder.configure({
    codec: 'opus',
    sampleRate: RATE,
    numberOfChannels: 1,
    bitrate: 32_000,
    opus: { frameDuration: 20_000, useinbandfec: true },
  } as AudioEncoderConfig);
  const toFrames = framer(FRAME, (frame) => {
    if (closed || encoder.state !== 'configured') return;
    const data = new AudioData({
      format: 'f32-planar',
      sampleRate: RATE,
      numberOfFrames: FRAME,
      numberOfChannels: 1,
      timestamp: outAt,
      data: frame,
    });
    outAt += (FRAME / RATE) * 1e6;
    encoder.encode(data);
    data.close();
  });

  let last = 'none';
  let texts = 0;
  let frames = 0;
  ws.addEventListener('message', (m: MessageEvent<ArrayBuffer | string>) => {
    if (closed) return;
    if (typeof m.data === 'string') {
      texts++;
      const type = (() => {
        try {
          return JSON.parse(m.data) as { type?: string; state?: TMachineState };
        } catch {
          return {};
        }
      })();
      if (type.type === 'relay.state' && type.state) {
        last = type.state;
        on.state(type.state);
      } else on.event(m.data);
      return;
    }
    frames++;
    if (decoder.state !== 'configured') return;
    decoder.decode(new EncodedAudioChunk({ type: 'key', timestamp: inAt, data: m.data }));
    inAt += (FRAME / RATE) * 1e6;
  });
  ws.addEventListener('close', () => {
    if (!closed) on.state(MACHINE_STATE.closed);
  });

  const close = () => {
    if (closed) return;
    closed = true;
    untap();
    for (const src of playing) src.stop();
    playing.clear();
    ws.close();
    for (const c of [encoder, decoder]) if (c.state !== 'closed') c.close();
    void ctx.close();
  };
  try {
    untap = await tapMic(ctx, mic, toFrames);
  } catch (error) {
    close();
    throw error;
  }
  if (ctx.state === 'suspended') void ctx.resume();
  return {
    stream: dest.stream,
    send: (event) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(event);
    },
    open: () => ws.readyState === WebSocket.OPEN,
    seen: () => `machine=${last} texts=${texts} frames=${frames}`,
    resume: () => void ctx.resume(),
    close,
  };
};

const PREFER_KEY = 'harnessly.voice.overMachine';
/** How long a device that needed the machine goes there first, before it tries the direct way again. */
const PREFER_MS = 3 * 24 * 3600_000;

/** Whether this device's last call needed the machine (not long ago). */
export const prefersMachine = (now = Date.now()): boolean => {
  try {
    const at = Number(localStorage.getItem(PREFER_KEY));
    return at > 0 && now - at < PREFER_MS;
  } catch {
    return false;
  }
};

/** Remembers on this device that its calls go over the machine (true) or the direct way again (false). */
export const preferMachine = (on: boolean, now = Date.now()) => {
  try {
    if (on) localStorage.setItem(PREFER_KEY, String(now));
    else localStorage.removeItem(PREFER_KEY);
  } catch {
    // A private window keeps nothing: the next call tries the direct way first again.
  }
};
