// The extra model vendors (Kimi, GLM, Grok) as the Settings page and the first-run step draw them. The runtime lists
// the presets (GET /api/v2/providers): each has an address and models of its own, fixed there; the person picks a
// preset and gives its key. The key is never read back: a saved one shows as its last digits.
import type { ProfileInfo, ProfileTestReason, ProfileVendor } from '@/next/protocol/v2';

export const VENDORS = ['kimi', 'glm', 'grok'] as const satisfies readonly ProfileVendor[];
export type Vendor = ProfileVendor;
export type IPresetState = ProfileInfo;
export type PresetId = ProfileInfo['id'];

/** Why a check of the connection did not pass: the runtime's reasons, and `page` when the check itself did not run. */
export type TestFailure = ProfileTestReason | 'page';

export type TestState = { k: 'idle' } | { k: 'testing' } | { k: 'ok' } | { k: 'failed'; reason: TestFailure };

/** The ways a preset is changed from the page; each resolves with the reason when the machine did not take it. */
export interface IPresetActions {
  /** Saves the key; resolves null when taken. */
  onSave: (preset: PresetId, key: string) => Promise<string | null>;
  onRemove: (preset: PresetId) => Promise<string | null>;
  onTest: (preset: PresetId) => Promise<TestState>;
}

/** The presets of a vendor in the order the runtime lists them. */
export const presetsOfVendor = (vendor: Vendor, presets: readonly IPresetState[]): IPresetState[] =>
  presets.filter((p) => p.vendor === vendor);

/** The vendors the runtime has presets of, in the page's order. */
export const vendorsOf = (presets: readonly IPresetState[]): Vendor[] =>
  VENDORS.filter((vendor) => presets.some((p) => p.vendor === vendor));

/** The preset a card opens on: the one that has a key, else the vendor's first. */
export const defaultPreset = (vendor: Vendor, presets: readonly IPresetState[]): PresetId | null => {
  const own = presetsOfVendor(vendor, presets);
  return (own.find((p) => p.key.configured) ?? own[0])?.id ?? null;
};

/** What tells a preset from its vendor's others in a segment: «Kimi · платформа» is «Платформа», «Grok» stays. */
export const presetName = (preset: IPresetState): string => {
  const [, ...rest] = preset.label.split(' · ');
  const name = rest.length > 0 ? rest.join(' · ') : preset.label;
  return `${name.charAt(0).toUpperCase()}${name.slice(1)}`;
};

/** A key as the field accepts it: trimmed. */
export const cleanKey = (raw: string): string => raw.trim();
/** The runtime takes 8–512 printable characters without a space. */
export const isKeyShape = (key: string): boolean =>
  key.length >= 8 && key.length <= 512 && /^[^\s\p{C}]+$/u.test(key);
