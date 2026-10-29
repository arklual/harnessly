// Whether the deck's PDF is there for the guest. The owner shares the link before the PDF is built (the runtime builds
// it in the background), so a page opened earlier asks for it again, through the link's own media address, until it is
// there; from then on the download always gets the latest build. The PDF may never come (the owner does not build it), so
// the wait between asks doubles from 15 s to 5 min, and a hidden tab does not ask. A link revoked meanwhile stops the
// asking and leaves for the gateway's page about it; a link that no longer gives the PDF (403) just stops.
import { useEffect, useState } from 'react';
import { pdfAllowed } from '@/next/features/designly/deck/deck-link-view';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';
import { LINK_GONE, leaveGoneLink } from '@/next/features/designly/guest/guest-gone';

export const GUEST_PDF_MS = 15_000;
export const GUEST_PDF_MAX_MS = 300_000;

/** The PDF's address while the guest may download it; null while it is not allowed or not built yet. */
export const useGuestPdf = (config: IGuestConfig): string | null => {
  const href = `${config.mediaBase}deck.pdf`;
  const published = config.deck?.pdf === true;
  const token = config.token;
  const [found, setFound] = useState(false);
  // A link on a snapshot or on some of the pages has no deck: there is no PDF to wait for.
  const waiting = config.deck !== null && !published && !found && pdfAllowed(config.view, true);
  useEffect(() => {
    if (!waiting) return;
    const abort = new AbortController();
    let gap = GUEST_PDF_MS;
    let timer = 0;
    const next = () => {
      timer = window.setTimeout(ask, gap);
      gap = Math.min(gap * 2, GUEST_PDF_MAX_MS);
    };
    const ask = () => {
      if (document.hidden) return next();
      fetch(href, { headers: { Range: 'bytes=0-0' }, cache: 'no-store', signal: abort.signal }).then(
        (res) => {
          void res.body?.cancel();
          if (res.ok) return setFound(true);
          if (res.status === LINK_GONE) return leaveGoneLink(token);
          if (res.status !== 403) next();
        },
        () => {
          if (!abort.signal.aborted) next();
        },
      );
    };
    next();
    return () => {
      abort.abort();
      window.clearTimeout(timer);
    };
  }, [waiting, href, token]);
  return pdfAllowed(config.view, published || found) ? href : null;
};
