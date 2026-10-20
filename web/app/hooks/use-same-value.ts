import { useState } from 'react';

/**
 * The value from the previous render while the new one has the same content, so a memoized consumer skips its render.
 * For derived lists that are rebuilt from the whole timeline on every message but rarely change.
 */
const useSameValue = <T>(value: T): T => {
  const key = JSON.stringify(value);
  const [kept, setKept] = useState({ key, value });
  if (kept.key === key) return kept.value;
  setKept({ key, value });
  return value;
};

export default useSameValue;
