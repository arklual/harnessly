// @vitest-environment jsdom
// «Сделай мне план на день» said in a ChatGPT chat (the 15.11 failure): the voice's task becomes a message of
// the chat, the chat writes the plan on screen, and only then the voice is told what is really there.
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IVoiceLiveControls } from '@/next/features/voice-live/voice-live';
import type { IVoiceTask } from '@/next/features/voice-live/use-voice-call';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/i18n', () => ({ t: (key: string) => key }));
const voice = vi.hoisted(() => ({
  onTask: null as null | ((task: IVoiceTask, controls: IVoiceLiveControls) => void),
}));
vi.mock('@/next/features/voice-live/voice-live', () => ({
  default: (p: { onTask?: (task: IVoiceTask, controls: IVoiceLiveControls) => void }) => {
    voice.onTask = p.onTask ?? null;
    return null;
  },
}));

const PLAN = '## План на день\n- 9:00 почта\n- 10:00 созвон\n- 14:00 доска «Путь покупателя»';
const api = vi.hoisted(() => ({
  messages: [] as { id: string; role: 'user' | 'assistant'; text: string; at: number }[],
  sent: [] as string[],
  done: null as null | (() => void),
}));
vi.mock('@/next/features/chatgpt/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/chatgpt/api')>()),
  loadChats: () => Promise.resolve([]),
  loadChat: () =>
    Promise.resolve({
      chat: { id: 'c1', title: '', createdAt: 0, updatedAt: 0, messages: [...api.messages] },
      running: false,
    }),
  sendMessage: (_m: MachineId, _id: string, body: { text: string }) => {
    api.sent.push(body.text);
    const message = { id: 'u1', role: 'user' as const, text: body.text, at: 1 };
    api.messages.push(message);
    return Promise.resolve({ message });
  },
  followAnswer: (_m: MachineId, _id: string, on: { onDone: () => void }) => {
    api.done = on.onDone;
    return () => undefined;
  },
  appendSpoken: () => Promise.resolve({}),
}));

import VoiceLiveGpt from '@/next/bindings/voice-live-gpt';
import type { ConvId } from '@/next/protocol/v2';

afterEach(cleanup);

describe('a task said in a ChatGPT chat', () => {
  it('is written by the chat, and the voice tells only what is on screen', async () => {
    // The voice lives in the app's layer, not in the chat's screen: it is the same away from the chat.
    render(<VoiceLiveGpt machine={'m1' as MachineId} conv={'gpt:c1' as ConvId} id="c1" />);
    await act(async () => {});
    expect(voice.onTask).not.toBeNull();
    const said: { text: string; task?: string }[] = [];
    const controls: IVoiceLiveControls = {
      say: (text, task) => {
        said.push({ text, task });
        return Promise.resolve(true);
      },
      note: () => undefined,
    };
    await act(async () => voice.onTask?.({ id: 'del_1', text: 'Составь план на день' }, controls));
    // The chat got the task as a message and started answering; the voice has been told nothing yet.
    expect(api.sent).toEqual(['Составь план на день']);
    expect(api.done).not.toBeNull();
    expect(said).toEqual([]);
    // The answer is over: the plan is in the chat, and the voice hears it.
    api.messages.push({ id: 'a1', role: 'assistant', text: PLAN, at: 2 });
    await act(async () => api.done?.());
    await act(async () => {});
    expect(said).toHaveLength(1);
    expect(said[0].task).toBe('del_1');
    expect(said[0].text).toContain('9:00 почта');
    expect(said[0].text).not.toContain('##');
  });
});
