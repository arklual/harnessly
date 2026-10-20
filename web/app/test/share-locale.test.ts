import { describe, expect, it } from 'vitest';
import { createRedactor } from '@/lib/hl-share-redact';
import { DEFAULT_SHARE_OPTIONS, projectTimeline } from '@/lib/hl-share-snapshot';
import type { ITimelineEntry } from '@/types/timeline';

const redactor = () => createRedactor({ knownSecrets: [], homeDirs: [], workspaceDir: null });

describe('share snapshot texts follow the page language', () => {
  it('writes the placeholder in English by default', () => {
    expect(redactor().redact('password=hunter2hunter2')).toBe('password=[hidden: kv-secret]');
  });

  it('leaves a value hidden in either language as it is', () => {
    for (const text of [
      'Authorization: Bearer [hidden: bearer]',
      'Authorization: Bearer [скрыто: bearer]',
      'password=[hidden: kv-secret]',
      'password=[скрыто: kv-secret]',
    ]) {
      expect(redactor().redact(text)).toBe(text);
    }
  });

  it('writes the notes of a snapshot in English by default', () => {
    const at = 1_000;
    const timeline = [
      { type: 'interrupt', timestamp: at },
      { type: 'context-compacted', timestamp: at },
      { type: 'error-notice', timestamp: at, severity: 'error', message: 'boom' },
    ] as unknown as ITimelineEntry[];
    const notes = projectTimeline(timeline, DEFAULT_SHARE_OPTIONS).map((entry) =>
      entry.t === 'note' ? entry.text : null,
    );
    expect(notes).toEqual(['Turn interrupted', 'Conversation context compacted', 'Error: boom']);
  });
});
