import { useEffect, useRef, useState } from 'react';

import RFB from '@novnc/novnc';
import { Button, Spinner } from '@kit';

import { t } from '../lib/i18n';
import './browser.css';

export const browserSocketUrl = (
  machineId: string,
  current: Pick<Location, 'protocol' | 'host'> = window.location,
): string => {
  const protocol = current.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${current.host}/api/machines/${encodeURIComponent(machineId)}/browser/ws`;
};

export const BrowserScreen = ({ machineId }: { machineId: string }) => {
  const target = useRef<HTMLDivElement>(null);
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<'connecting' | 'connected' | 'error'>('connecting');

  useEffect(() => {
    const element = target.current;
    if (!element) return;
    let active = true;
    let rfb: RFB | null = null;
    setState('connecting');

    // StrictMode immediately replays effects in development. Deferring construction lets its first
    // cleanup cancel that attempt, so only the surviving effect opens a socket.
    void Promise.resolve().then(() => {
      if (!active) return;
      try {
        rfb = new RFB(element, browserSocketUrl(machineId));
        rfb.scaleViewport = true;
        rfb.viewOnly = false;
        rfb.focusOnClick = true;
        rfb.addEventListener('connect', () => {
          if (active) setState('connected');
        });
        rfb.addEventListener('disconnect', () => {
          if (active) setState('error');
        });
      } catch {
        if (active) setState('error');
      }
    });

    return () => {
      active = false;
      rfb?.disconnect();
    };
  }, [machineId, attempt]);

  return (
    <main className="hub-browser">
      <div ref={target} className="hub-browser__screen" />
      {state === 'connecting' && (
        <div className="hub-browser__status" role="status">
          <Spinner size="s" />
          {t('browser.connecting')}
        </div>
      )}
      {state === 'error' && (
        <div className="hub-browser__status" role="alert">
          <span>{t('browser.failed')}</span>
          <Button onClick={() => setAttempt((n) => n + 1)}>{t('common.retryAgain')}</Button>
        </div>
      )}
    </main>
  );
};
