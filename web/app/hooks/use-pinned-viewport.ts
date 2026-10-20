import { useEffect } from 'react';

/**
 * The workspace is a fixed full-screen layout: the document itself never scrolls. iOS scrolls it anyway
 * to lift a focused field above the keyboard and, in a Home Screen app, leaves it there once the
 * keyboard is gone: everything sits shifted up with an empty strip at the bottom.
 */
const usePinnedViewport = (enabled: boolean) => {
  useEffect(() => {
    if (!enabled) return;
    const pin = () => {
      // While typing, that scroll is what keeps the field above the keyboard.
      const active = document.activeElement;
      if (
        active instanceof HTMLElement &&
        (active.isContentEditable || active.tagName === 'TEXTAREA' || active.tagName === 'INPUT')
      )
        return;
      if (window.scrollY !== 0 || window.scrollX !== 0) window.scrollTo(0, 0);
    };
    let timer = 0;
    const later = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(pin, 60);
    };
    const viewport = window.visualViewport;
    viewport?.addEventListener('resize', later);
    window.addEventListener('focusout', later);
    return () => {
      window.clearTimeout(timer);
      viewport?.removeEventListener('resize', later);
      window.removeEventListener('focusout', later);
    };
  }, [enabled]);
};

export default usePinnedViewport;
