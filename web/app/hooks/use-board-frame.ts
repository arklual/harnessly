// The address a board tab frames. With a boards service and a token on the runtime, the runtime mints a one-time
// sign-in for the person (POST /api/integrations/boards/sign-in) and the frame opens the editor already signed in;
// otherwise, or when the hand-off fails, the board's own address (read-only for a guest).
import { useEffect, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import { boardsOrigin } from '@/lib/site';

const ownBoard = (url: string): URL | null => {
  const origin = boardsOrigin();
  try {
    const target = new URL(url);
    return origin && target.origin === origin ? target : null;
  } catch {
    return null;
  }
};

export default function useBoardFrame(url: string): string | null {
  const [frame, setFrame] = useState<{ for: string; src: string } | null>(null);
  const target = ownBoard(url);
  useEffect(() => {
    if (!target) return;
    const controller = new AbortController();
    void appFetch('/api/integrations/boards/sign-in', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ next: `${target.pathname}${target.search}` }),
      signal: controller.signal,
    })
      .then(async (response) => {
        const body = response.ok ? ((await response.json()) as { url?: string }) : null;
        setFrame({ for: url, src: body?.url ?? url });
      })
      .catch(() => {
        if (!controller.signal.aborted) setFrame({ for: url, src: url });
      });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `target` is derived from `url`
  }, [url]);
  if (!target) return url;
  return frame?.for === url ? frame.src : null;
}
