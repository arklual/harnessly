/** Allow-list for /api/voice/assets: exactly the Whisper model and ONNX Runtime files, nothing else. */

export const VOICE_WHISPER_MODEL = 'onnx-community/whisper-small';

export interface IVoiceAsset {
  upstreamUrl: string;
  cacheKey: string;
  contentType: string;
}

const MODEL_FILE =
  /^(?:[a-z_]+\.(?:json|txt)|onnx\/(?:encoder_model|decoder_model_merged)(?:_fp16|_q4|_quantized)?\.onnx(?:_data)?)$/;
const ORT_FILE = /^ort-wasm-simd-threaded\.jsep\.(?:mjs|wasm)$/;
const VERSION = /^\d+\.\d+\.\d+$/;

const contentTypeOf = (file: string): string => {
  if (file.endsWith('.json')) return 'application/json';
  if (file.endsWith('.txt')) return 'text/plain; charset=utf-8';
  if (file.endsWith('.wasm')) return 'application/wasm';
  if (file.endsWith('.mjs')) return 'text/javascript';
  return 'application/octet-stream';
};

/**
 * `hf/<org>/<model>/resolve/main/<file>` mirrors the Hugging Face URL layout transformers.js builds from
 * `env.remoteHost`; `ort/<transformers version>/<file>` mirrors the jsDelivr dist it would otherwise use.
 */
export const resolveVoiceAsset = (segments: readonly string[]): IVoiceAsset | null => {
  const [kind, ...rest] = segments;
  if (kind === 'hf') {
    const modelPrefix = `${VOICE_WHISPER_MODEL}/resolve/main/`;
    const joined = rest.join('/');
    if (!joined.startsWith(modelPrefix)) return null;
    const file = joined.slice(modelPrefix.length);
    if (!MODEL_FILE.test(file)) return null;
    return {
      upstreamUrl: `https://huggingface.co/${modelPrefix}${file}`,
      cacheKey: `hf/${VOICE_WHISPER_MODEL}/${file}`,
      contentType: contentTypeOf(file),
    };
  }
  if (kind === 'ort' && rest.length === 2) {
    const [version, file] = rest;
    if (!VERSION.test(version) || !ORT_FILE.test(file)) return null;
    return {
      upstreamUrl: `https://cdn.jsdelivr.net/npm/@huggingface/transformers@${version}/dist/${file}`,
      cacheKey: `ort/${version}/${file}`,
      contentType: contentTypeOf(file),
    };
  }
  return null;
};
