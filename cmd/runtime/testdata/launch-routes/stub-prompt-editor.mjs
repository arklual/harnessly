// The prompt editor of the previous runtime, except that a submitted text is recorded instead of typed.
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const real = await import(pathToFileURL(path.join(process.env.OLD_RUNTIME_SRC, 'src/lib', `${process.env.OLD_MODULE_PREFIX}-prompt-editor.ts`)).href);
export const withPromptEditor = real.withPromptEditor;
export const promptEditorEnvPrefix = real.promptEditorEnvPrefix;
export const ensurePromptEditorScript = real.ensurePromptEditorScript;
export const promptEditorScriptPath = real.promptEditorScriptPath;
export const submitPromptText = async (session, text) => { globalThis.__golden.calls.push(['submit', session, text]); return 'editor'; };
