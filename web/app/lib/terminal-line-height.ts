/** Terminal line height: a named preset or a custom value kept between 1 and 2. */
const PRESETS: Record<string, number> = { compact: 1, tight: 1, normal: 1.1, relaxed: 1.3 };

export const resolveLineHeight = (preset: string, custom: number): number => {
  if (preset === 'custom')
    return Number.isFinite(custom) ? Math.min(2, Math.max(1, custom)) : PRESETS.normal!;
  return PRESETS[preset] ?? PRESETS.normal!;
};
