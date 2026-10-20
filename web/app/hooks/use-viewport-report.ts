import { useEffect } from 'react';
import { appFetch } from '@/lib/app-path';

const probe = (style: Partial<CSSStyleDeclaration>) => {
  const el = document.createElement('div');
  Object.assign(
    el.style,
    { position: 'fixed', left: '0', top: '0', width: '0', visibility: 'hidden', pointerEvents: 'none' },
    style,
  );
  document.body.appendChild(el);
  const rect = el.getBoundingClientRect();
  const computed = getComputedStyle(el);
  const result = {
    height: rect.height,
    top: parseFloat(computed.paddingTop),
    bottom: parseFloat(computed.paddingBottom),
  };
  el.remove();
  return result;
};

const measure = () => {
  const root = document.querySelector('.tw-root')?.getBoundingClientRect();
  const composer = document.querySelector('[contenteditable]')?.getBoundingClientRect();
  const insets = probe({
    height: '0',
    paddingTop: 'env(safe-area-inset-top)',
    paddingBottom: 'env(safe-area-inset-bottom)',
  });
  return {
    innerHeight: window.innerHeight,
    outerHeight: window.outerHeight,
    screenHeight: window.screen.height,
    clientHeight: document.documentElement.clientHeight,
    visualHeight: window.visualViewport?.height,
    visualTop: window.visualViewport?.offsetTop,
    scrollY: window.scrollY,
    dvh: probe({ height: '100dvh' }).height,
    lvh: probe({ height: '100lvh' }).height,
    svh: probe({ height: '100svh' }).height,
    vh: probe({ height: '100vh' }).height,
    safeTop: insets.top,
    safeBottom: insets.bottom,
    rootTop: root?.top,
    rootBottom: root?.bottom,
    composerBottom: composer?.bottom,
    dpr: window.devicePixelRatio,
  };
};

/** The phone's Home Screen app: its viewport numbers go to the runtime log at launch, after the first touch and on resizes. */
const useViewportReport = (enabled: boolean) => {
  useEffect(() => {
    if (!enabled || !window.matchMedia('(display-mode: standalone)').matches) return;
    const send = (moment: string) => {
      void appFetch('/api/push/viewport', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ moment, standalone: true, userAgent: navigator.userAgent, sizes: measure() }),
      }).catch(() => {});
    };
    let resizes = 0;
    const resized = () => {
      if (++resizes <= 3) send('resize');
    };
    window.visualViewport?.addEventListener('resize', resized);
    const timers = [
      window.setTimeout(() => send('launch'), 1500),
      window.setTimeout(() => send('settled'), 8000),
    ];
    let touched = false;
    const afterTouch = () => {
      if (touched) return;
      touched = true;
      window.setTimeout(() => send('after-touch'), 800);
    };
    window.addEventListener('touchend', afterTouch, { passive: true });
    return () => {
      timers.forEach((timer) => window.clearTimeout(timer));
      window.visualViewport?.removeEventListener('resize', resized);
      window.removeEventListener('touchend', afterTouch);
    };
  }, [enabled]);
};

export default useViewportReport;
