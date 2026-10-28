// Whether the deck's PDF is there for the guest. The owner shares the link before the PDF is built (the runtime builds
// it in the background), so a page opened earlier asks for it again every 15 s, through the link's own media address,
// until it is there; from then on the download always gets the latest build.
import { useEffect, useState } from 'react';
import { pdfAllowed } from '@/next/features/designly/deck/deck-link-view';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';

export const GUEST_PDF_MS = 15_000;

/** The PDF's address while the guest may download it; null while it is not allowed or not built yet. */
export const useGuestPdf = (config: IGuestConfig): string | null => {
  const href = `${config.mediaBase}deck.pdf`;
  const published = config.deck?.pdf === true;
  const [found, setFound] = useState(false);
  // A link on a snapshot or on some of the pages has no deck: there is no PDF to wait for.
  const waiting = config.deck !== null && !published && !found && pdfAllowed(config.view, true);
  useEffect(() => {
    if (!waiting) return;
    const abort = new AbortController();
    const ask = () =>
      void fetch(href, { headers: { Range: 'bytes=0-0' }, cache: 'no-store', signal: abort.signal }).then(
        (res) => {
          void res.body?.cancel();
          if (res.ok) setFound(true);
        },
        () => undefined,
      );
    const timer = window.setInterval(ask, GUEST_PDF_MS);
    return () => {
      abort.abort();
      window.clearInterval(timer);
    };
  }, [waiting, href]);
  return pdfAllowed(config.view, published || found) ? href : null;
};
