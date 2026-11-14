import { describe, expect, it, vi } from 'vitest';
import { askUIAgent, readUIFrames, settingPlaces, UI_EVENT } from '@/next/features/settings/ui-agent-api';
import type { MachineId } from '@/next/protocol/v2';
import { pinLocale } from '@/next/i18n';

pinLocale('ru');

describe('the window agent', () => {
  it("names the settings by their pages, at their section's path, without the hidden ones", () => {
    const places = settingPlaces(['voice']);
    const enter = places.find((p) => p.title === 'Основные → Enter отправляет');
    expect(enter?.id).toBe('/settings/general');
    expect(places.some((p) => p.id === '/settings/voice')).toBe(false);
  });

  it('reads the action before the words', () => {
    const { events, rest } = readUIFrames(
      'event: action\ndata: {"kind":"go","place":"/settings/plugins","label":"Плагины"}\n\n' +
        'event: delta\ndata: {"text":"Открыл"}\n\n' +
        'event: action\ndata: {"kind":"confirm","key":"k","value":"off","label":"Выкл","title":"T","was":"Вкл"}\n\n' +
        'event: done\ndata: {"text":"Открыл."}\n\nevent: del',
    );
    expect(events.map((e) => e.type)).toEqual([UI_EVENT.go, UI_EVENT.delta, UI_EVENT.confirm, UI_EVENT.done]);
    expect(rest).toBe('event: del');
  });
});

describe('the language of the answer', () => {
  it("is the interface's, sent with the request", async () => {
    pinLocale('en');
    let sent: Record<string, unknown> = {};
    vi.stubGlobal('fetch', (_url: string, init: RequestInit) => {
      sent = JSON.parse(String(init.body)) as Record<string, unknown>;
      return Promise.resolve(new Response('{"error":"no-model"}', { status: 409 }));
    });
    const events: string[] = [];
    await askUIAgent(
      'm1' as MachineId,
      { query: 'открой плагины', page: '/settings', map: '', places: [] },
      new AbortController().signal,
      (event) => events.push(event.type),
    );
    vi.unstubAllGlobals();
    pinLocale('ru');
    expect(sent.lang).toBe('en');
    expect(events).toEqual([UI_EVENT.error]);
  });
});
