// The ChatGPT integration's routes on a machine (cmd/runtime/routes_chatgpt.go): the sign-in with ChatGPT,
// the models of the plan and the conversations. A token never comes to the page: the machine keeps them in its safe.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { postJson, runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import type {
  IGptAccount,
  IGptChat,
  IGptChatSummary,
  IGptMessage,
  IGptModel,
  IGptState,
  IGptUpload,
} from '@/next/features/chatgpt/model';

const path = (tail = ''): `/${string}` => platformPath(`/chatgpt${tail}` as `/${string}`);

const send = (method: 'PATCH' | 'DELETE' | 'POST', body?: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  ...(body === undefined ? {} : { body: JSON.stringify(body) }),
});

/** The machine's code of a refusal (`signed-out`, `usage-limit`…), '' when it said none. */
export const refusalCode = (failure: unknown): string => {
  if (!(failure instanceof RuntimeError)) return '';
  const body = failure.body;
  return typeof body === 'object' && body !== null && 'error' in body ? String(body.error) : '';
};

export const loadState = (m: MachineId) => runtimeJson<IGptState>(m, path());

export const startLogin = (m: MachineId, account?: string) =>
  runtimeJson<{ url: string; redirect: string }>(m, path('/login'), postJson(account ? { account } : {}));

export const finishLogin = (m: MachineId, url: string) =>
  runtimeJson<{ account: IGptAccount }>(m, path('/login/callback'), postJson({ url }));

export const cancelLogin = (m: MachineId) => runtimeJson<{ ok: boolean }>(m, path('/login'), send('DELETE'));

export const signOut = (m: MachineId, key: string) =>
  runtimeJson<{ revoked: boolean }>(m, path(`/accounts/${encodeURIComponent(key)}/logout`), send('POST'));

export const forgetAccount = (m: MachineId, key: string) =>
  runtimeJson<{ revoked: boolean }>(m, path(`/accounts/${encodeURIComponent(key)}`), send('DELETE'));

export const activateAccount = (m: MachineId, key: string) =>
  runtimeJson<{ ok: boolean }>(m, path(`/accounts/${encodeURIComponent(key)}/active`), send('POST'));

export const loadModels = (m: MachineId) =>
  runtimeJson<{ models: IGptModel[] | null }>(m, path('/models')).then((a) => a.models ?? []);

export const loadChats = (m: MachineId) =>
  runtimeJson<{ chats: IGptChatSummary[] | null }>(m, path('/chats')).then((a) => a.chats ?? []);

export const createChat = (m: MachineId, model: string, effort: string) =>
  runtimeJson<{ chat: IGptChat }>(m, path('/chats'), postJson({ model, effort })).then((a) => a.chat);

export const loadChat = (m: MachineId, id: string) =>
  runtimeJson<{ chat: IGptChat; running: boolean }>(m, path(`/chats/${id}`));

export const renameChat = (m: MachineId, id: string, title: string) =>
  runtimeJson<{ ok: boolean }>(m, path(`/chats/${id}`), send('PATCH', { title }));

export const deleteChat = (m: MachineId, id: string) =>
  runtimeJson<{ ok: boolean }>(m, path(`/chats/${id}`), send('DELETE'));

export const sendMessage = (
  m: MachineId,
  id: string,
  body: { text: string; model: string; effort: string; files: readonly IGptUpload[] },
) => runtimeJson<{ message: IGptMessage }>(m, path(`/chats/${id}/messages`), postJson(body), 120_000);

export const stopAnswer = (m: MachineId, id: string) =>
  runtimeJson<{ stopped: boolean }>(m, path(`/chats/${id}/stop`), send('POST'));

/** The address of an attachment kept with a chat. */
export const fileUrl = (m: MachineId, chat: string, file: string) =>
  runtimePath(path(`/chats/${chat}/files/${file}`), m);

export interface IAnswerHandlers {
  onDelta: (text: string) => void;
  onDone: (message: IGptMessage | null) => void;
}

/** Follows the answer being written in a chat; the returned function stops following. */
export const followAnswer = (m: MachineId, id: string, on: IAnswerHandlers): (() => void) => {
  const source = new EventSource(runtimePath(path(`/chats/${id}/stream`), m));
  let over = false;
  const end = (message: IGptMessage | null) => {
    if (over) return;
    over = true;
    source.close();
    on.onDone(message);
  };
  source.addEventListener('delta', (e) => {
    const data = JSON.parse((e as MessageEvent<string>).data) as { text?: string };
    if (data.text) on.onDelta(data.text);
  });
  source.addEventListener('done', (e) => {
    const data = JSON.parse((e as MessageEvent<string>).data) as { message?: IGptMessage };
    end(data.message ?? null);
  });
  source.addEventListener('idle', () => end(null));
  // The stream broke (the machine went away): what was kept is read again by the caller.
  source.onerror = () => end(null);
  return () => {
    over = true;
    source.close();
  };
};

/** Keeps what was said in the voice mode as the chat's messages. */
export const appendSpoken = (
  m: MachineId,
  id: string,
  lines: readonly { role: 'user' | 'assistant'; text: string }[],
) => runtimeJson<{ messages: IGptMessage[] }>(m, path(`/chats/${id}/spoken`), postJson({ lines }));
