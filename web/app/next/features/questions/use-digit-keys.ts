// Digits 1–9 pick an option of the open gate, as in Claude Code. They act when the focus is not in a field, or the
// field is empty (the composer usually has the focus), and never with a modifier.
import { useEffect, useRef } from 'react';

const isEditable = (
  target: EventTarget | null,
): target is HTMLInputElement | HTMLTextAreaElement | HTMLElement => {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA';
};

const hasText = (target: EventTarget | null): boolean => {
  if (!isEditable(target)) return false;
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)
    return target.value.length > 0;
  return (target.textContent ?? '').length > 0;
};

/** Calls `onDigit(n)` (1-based) for a free digit key while `on`. */
const useDigitKeys = (on: boolean, count: number, onDigit: (n: number) => void) => {
  const latest = useRef(onDigit);
  useEffect(() => {
    latest.current = onDigit;
  });
  useEffect(() => {
    if (!on) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey || event.repeat) return;
      if (!/^[1-9]$/.test(event.key)) return;
      const n = Number(event.key);
      if (n > count || hasText(event.target)) return;
      event.preventDefault();
      latest.current(n);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [on, count]);
};

export default useDigitKeys;
