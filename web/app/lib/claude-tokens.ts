/** A Claude model id as people read it: `claude-opus-4-5-20251101` → `Opus 4.5`. */
export const formatModelDisplayName = (modelId: string): string => {
  const m = /^claude-(?:(\d+(?:-\d(?!\d))?)-)?([a-z]+)(?:-(\d+(?:-\d(?!\d))?))?/i.exec(modelId);
  if (!m) return modelId;
  const family = m[2]!.charAt(0).toUpperCase() + m[2]!.slice(1);
  const version = (m[3] ?? m[1] ?? '').replace(/-/g, '.');
  return version ? `${family} ${version}` : family;
};
