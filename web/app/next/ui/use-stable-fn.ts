// A function whose identity never changes and which always runs the latest version given: for a handler passed to a
// memoized child, so the child does not draw again every time its parent does.
import { useCallback, useLayoutEffect, useRef } from 'react';

export const useStableFn = <A extends unknown[], R>(fn: (...args: A) => R): ((...args: A) => R) => {
  const latest = useRef(fn);
  useLayoutEffect(() => {
    latest.current = fn;
  });
  return useCallback((...args: A) => latest.current(...args), []);
};
