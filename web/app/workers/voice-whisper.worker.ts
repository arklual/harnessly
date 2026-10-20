/// <reference lib="webworker" />
import { env, pipeline, type AutomaticSpeechRecognitionPipeline } from '@huggingface/transformers';
import { VOICE_WHISPER_MODEL } from '@/lib/voice-assets';
import { WHISPER_LANGUAGE, WHISPER_SAMPLE_RATE, cleanWhisperText } from '@/lib/voice-whisper-text';
import type { TVoiceLang } from '@/lib/voice-settings';

/**
 * In-tab Whisper: the model runs in this worker on WebGPU where the GPU supports fp16, otherwise on
 * WASM; audio never leaves the browser. Files come from our own /api/voice/assets and are kept in the browser's
 * Cache API after the first download.
 */

export type TWhisperWorkerRequest =
  | { type: 'load'; assetsBase: string }
  | { type: 'transcribe'; id: number; audio: Float32Array; lang: TVoiceLang };

export type TWhisperWorkerResponse =
  | { type: 'progress'; loaded: number; total: number }
  | { type: 'ready'; device: 'webgpu' | 'wasm' }
  | { type: 'result'; id: number; text: string }
  | { type: 'error'; id?: number; message: string };

const ctx = self as unknown as DedicatedWorkerGlobalScope;
const post = (message: TWhisperWorkerResponse) => ctx.postMessage(message);

let loading: Promise<AutomaticSpeechRecognitionPipeline> | null = null;

// transformers.js's overloaded `pipeline` type is too large for tsc to infer (TS2590); pin the one overload used.
const createAsr = pipeline as unknown as (
  task: 'automatic-speech-recognition',
  model: string,
  options: Record<string, unknown>,
) => Promise<AutomaticSpeechRecognitionPipeline>;

const hasFp16Gpu = async (): Promise<boolean> => {
  const gpu = (
    navigator as Navigator & { gpu?: { requestAdapter: () => Promise<{ features: Set<string> } | null> } }
  ).gpu;
  if (!gpu) return false;
  const adapter = await gpu.requestAdapter().catch(() => null);
  return !!adapter?.features.has('shader-f16');
};

const load = (assetsBase: string): Promise<AutomaticSpeechRecognitionPipeline> => {
  if (loading) return loading;
  env.allowLocalModels = false;
  env.remoteHost = `${assetsBase}hf/`;
  const ortBase = `${assetsBase}ort/${env.version}/`;
  const onnx = env.backends.onnx as { wasm?: { wasmPaths?: unknown } };
  if (onnx.wasm) {
    onnx.wasm.wasmPaths = {
      mjs: `${ortBase}ort-wasm-simd-threaded.jsep.mjs`,
      wasm: `${ortBase}ort-wasm-simd-threaded.jsep.wasm`,
    };
  }

  const files = new Map<string, { loaded: number; total: number }>();
  let lastPercent = -1;
  const progress_callback = (event: { status: string; file?: string; loaded?: number; total?: number }) => {
    if (event.status !== 'progress' || !event.file) return;
    files.set(event.file, { loaded: event.loaded ?? 0, total: event.total ?? 0 });
    let loaded = 0;
    let total = 0;
    for (const f of files.values()) {
      loaded += f.loaded;
      total += f.total;
    }
    // One message per whole percent: a download fires this per network chunk, and each message re-renders the composer.
    const percent = total > 0 ? Math.floor((loaded / total) * 100) : 0;
    if (percent === lastPercent) return;
    lastPercent = percent;
    post({ type: 'progress', loaded, total });
  };

  const create = (device: 'webgpu' | 'wasm') =>
    createAsr('automatic-speech-recognition', VOICE_WHISPER_MODEL, {
      device,
      dtype: device === 'webgpu' ? { encoder_model: 'fp16', decoder_model_merged: 'q4' } : 'q8',
      progress_callback,
    });

  loading = (async () => {
    if (await hasFp16Gpu()) {
      try {
        const asr = await create('webgpu');
        post({ type: 'ready', device: 'webgpu' });
        return asr;
      } catch {
        files.clear();
      }
    }
    const asr = await create('wasm');
    post({ type: 'ready', device: 'wasm' });
    return asr;
  })();
  loading.catch((err: unknown) => {
    loading = null;
    post({
      type: 'error',
      message: err instanceof Error ? err.message : String(err),
    });
  });
  return loading;
};

let assetsBaseSeen = '';

ctx.onmessage = async (event: MessageEvent<TWhisperWorkerRequest>) => {
  const msg = event.data;
  if (msg.type === 'load') {
    assetsBaseSeen = msg.assetsBase;
    void load(msg.assetsBase).catch(() => {});
    return;
  }
  try {
    const asr = await load(assetsBaseSeen);
    const language = WHISPER_LANGUAGE[msg.lang];
    const longForm =
      msg.audio.length > 30 * WHISPER_SAMPLE_RATE ? { chunk_length_s: 30, stride_length_s: 5 } : {};
    const output = await asr(msg.audio, {
      task: 'transcribe',
      ...longForm,
      ...(language ? { language } : {}),
    });
    const text = (Array.isArray(output) ? output.map((o) => o.text).join(' ') : output.text) ?? '';
    post({ type: 'result', id: msg.id, text: cleanWhisperText(text) });
  } catch (err) {
    post({
      type: 'error',
      id: msg.id,
      message: err instanceof Error ? err.message : String(err),
    });
  }
};
