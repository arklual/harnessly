// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { meStore, setMeSource, localMeSource, loadMe, DEFAULT_SETTINGS } from '@/next/stores/me';
import { planSync, startPrefsSync, SYNCED_PREFS } from '@/next/stores/synced-prefs';
import { notePrefWrite } from '@/next/core/pref-write';

describe('planSync', () => {
  it('moves the browser values into settings that have none yet', () => {
    const plan = planSync(
      { synced: false, values: {} },
      { 'hl-theme': 'dark', 'hl:beta': '{"retitle":true}', 'hx-top-tabs': null },
    );
    expect(plan).toEqual({ k: 'migrate', values: { 'hl-theme': 'dark', 'hl:beta': '{"retitle":true}' } });
  });

  it('takes the settings over the browser once they are synced, removing what they lack', () => {
    const plan = planSync(
      { synced: true, values: { 'hl-theme': 'light', 'voice.lang': 'en' } },
      { 'hl-theme': 'dark', 'hl:beta': '{"retitle":true}', 'voice.lang': 'en' },
    );
    expect(plan).toEqual({ k: 'apply', writes: { 'hl-theme': 'light', 'hl:beta': null } });
  });

  it('keeps device-only keys out', () => {
    expect(SYNCED_PREFS).not.toContain('hl:voice');
    expect(SYNCED_PREFS).not.toContain('hx-tabs');
  });
});

describe('startPrefsSync', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
  });

  it('migrates on first sign-in, then carries a write and lets the settings win on another device', async () => {
    localStorage.setItem('hl-theme', 'dark');
    localStorage.setItem('hl:voice', JSON.stringify({ deviceId: 'mic-1', lang: 'en' }));
    localStorage.setItem('hl:browser-agent', JSON.stringify({ blocked: ['bank.example.com'] }));
    setMeSource(localMeSource());
    await loadMe();
    startPrefsSync();
    await vi.waitFor(() => {
      const st = meStore.get();
      expect(st.k === 'ready' && st.me.settings.prefs.synced).toBe(true);
    });
    const st = meStore.get();
    if (st.k !== 'ready') throw new Error('not ready');
    expect(st.me.settings.prefs.values).toEqual({
      'hl-theme': 'dark',
      'voice.lang': 'en',
      'hl:browser-agent': '{"blocked":["bank.example.com"]}',
    });

    localStorage.setItem('hl-palette', 'forest');
    notePrefWrite('hl-palette');
    await vi.waitFor(() => {
      const now = meStore.get();
      expect(now.k === 'ready' && now.me.settings.prefs.values['hl-palette']).toBe('forest');
    });

    // Another device changed the theme and dropped the blocked site: this browser follows, the microphone stays.
    const now = meStore.get();
    if (now.k !== 'ready') throw new Error('not ready');
    meStore.set({
      k: 'ready',
      me: {
        ...now.me,
        settings: {
          ...DEFAULT_SETTINGS,
          prefs: {
            synced: true,
            values: { 'hl-theme': 'light', 'voice.lang': 'ru', 'hl-palette': 'forest' },
          },
        },
      },
    });
    await vi.waitFor(() => expect(localStorage.getItem('hl-theme')).toBe('light'));
    expect(localStorage.getItem('hl:browser-agent')).toBeNull();
    expect(JSON.parse(localStorage.getItem('hl:voice') ?? '{}')).toEqual({ deviceId: 'mic-1', lang: 'ru' });
  });
});
