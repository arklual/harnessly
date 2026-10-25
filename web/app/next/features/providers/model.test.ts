import { describe, expect, it } from 'vitest';
import {
  defaultPreset,
  isKeyShape,
  presetName,
  presetsOfVendor,
  vendorsOf,
  type IPresetState,
} from '@/next/features/providers/model';

const preset = (id: string, vendor: IPresetState['vendor'], label: string, tail: string | null = null) =>
  ({
    id,
    label,
    vendor,
    via: vendor === 'grok' ? 'codex' : 'claude',
    host: 'example.test',
    hint: '',
    key: { configured: tail !== null, tail },
    models: [],
  }) satisfies IPresetState;

const LIST = [
  preset('kimi', 'kimi', 'Kimi · платформа'),
  preset('kimi-code', 'kimi', 'Kimi · Kimi Code', '…3456'),
  preset('xai', 'grok', 'Grok'),
];

describe('presets', () => {
  it('groups the runtime’s presets by vendor in the runtime’s order', () => {
    expect(presetsOfVendor('kimi', LIST).map((p) => p.id)).toEqual(['kimi', 'kimi-code']);
  });

  it('lists only the vendors the runtime has presets of, in the page’s order', () => {
    expect(vendorsOf(LIST)).toEqual(['kimi', 'grok']);
    expect(vendorsOf([])).toEqual([]);
  });

  it('opens a card on the preset that has a key, else on the first, else on none', () => {
    expect(defaultPreset('kimi', LIST)).toBe('kimi-code');
    expect(defaultPreset('grok', LIST)).toBe('xai');
    expect(defaultPreset('glm', LIST)).toBeNull();
  });

  it('names a preset by what tells it from its vendor’s others', () => {
    expect(presetName(LIST[0])).toBe('Платформа');
    expect(presetName(LIST[2])).toBe('Grok');
  });
});

describe('a key as typed', () => {
  it('takes 8 to 512 printable characters without a space', () => {
    expect(isKeyShape('sk-test-1234567890')).toBe(true);
    expect(isKeyShape('sk-123')).toBe(false);
    expect(isKeyShape('sk-test 1234567890')).toBe(false);
    expect(isKeyShape('k'.repeat(513))).toBe(false);
  });
});
