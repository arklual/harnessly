import { appHost } from '@/lib/site';
import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { copyToClipboard } from '@/lib/clipboard';
import type { IDesignLocation } from '@/lib/hl-conversation-url';
import {
  OPEN_MODE,
  SHARE_MODE,
  chooseOpenMode,
  chooseShareMode,
  designLinkUrl,
  readDeviceEnv,
} from '@/lib/hl-design-link';
import { openDesignLocation } from '@/lib/hl-open-design';
import { t } from '@/lib/i18n';

const SHARED_MS = 1_500;
const copiedMessage = (): string => t('hooks.useDesignLink.copied', { host: appHost() });

/** What the button that takes a board out of the page is called: a tab where there are tabs, the whole screen where the window is all there is. */
export const openFullLabel = (): string =>
  chooseOpenMode(readDeviceEnv()) === OPEN_MODE.window
    ? t('hooks.useDesignLink.openInNewTab')
    : t('hooks.useDesignLink.openFullScreen');

const dismissed = (error: unknown): boolean => error instanceof DOMException && error.name === 'AbortError';

/**
 * What a person does with a place of the design library besides looking at it: send its link to a teammate
 * and take it out of the page into a window of its own.
 */
const useDesignLink = () => {
  const [shared, setShared] = useState(false);
  useEffect(() => {
    if (!shared) return;
    const timer = window.setTimeout(() => setShared(false), SHARED_MS);
    return () => window.clearTimeout(timer);
  }, [shared]);

  /** The share sheet on a phone, the clipboard everywhere else; a sheet that cannot open falls back to the clipboard and says so. */
  const share = useCallback(async (location: IDesignLocation, title: string) => {
    const url = designLinkUrl(window.location.origin, location);
    if (chooseShareMode(readDeviceEnv()) === SHARE_MODE.sheet) {
      try {
        await navigator.share({ title, url });
        return;
      } catch (error) {
        if (dismissed(error)) return;
      }
    }
    if (await copyToClipboard(url)) {
      setShared(true);
      toast.success(copiedMessage());
    } else {
      toast.error(t('hooks.useDesignLink.copyFailed'));
    }
  }, []);

  /** The board alone in a window of its own: a new tab on a computer, the current window on a phone or an installed app. */
  const openFull = useCallback((location: IDesignLocation) => {
    const full = { ...location, full: true };
    if (chooseOpenMode(readDeviceEnv()) === OPEN_MODE.here) openDesignLocation(full);
    else window.open(designLinkUrl(window.location.origin, full), '_blank', 'noopener');
  }, []);

  return { share, shared, openFull };
};

export default useDesignLink;
