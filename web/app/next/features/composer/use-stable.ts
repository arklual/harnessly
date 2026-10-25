import { useState } from 'react';

/** The same object while its key is the same: a value built anew on every render does not look like a change. */
export const useStable = <T>(value: T | null, key: string): T | null => {
  const [kept, setKept] = useState<{ key: string; value: T | null }>({ key, value });
  if (kept.key === key) return kept.value;
  setKept({ key, value });
  return value;
};
