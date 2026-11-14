// The wait for a design's page: in the middle of the screen where the page will be, the design's icon dimmed,
// a spinner round it and one short line that changes while the wait goes on («Загружаем дизайн…», «Готовим страницы…»).
// A long wait says so instead. Swap (ui/swap.tsx) shows it only after its usual delay and crossfades it with the page;
// it fades in itself, so a quick load never flashes it.
import { useEffect, useRef, useState } from 'react';
import { readBridgeMessage } from '@/next/features/designly/page-bridge';
import { LOAD_STAGE, type TLoadStage } from '@/next/features/designly/site-load';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

/** The lines of a usual wait, in turn. */
export const LOADER_LINES = ['next.site.loadDesign', 'next.site.loadPages', 'next.site.loadFiles'] as const;
/** How long a line stays before the next. */
const LINE_MS = 1_800;

/** The line for this moment of the wait: the usual ones go round; a long wait has its own. */
export const loaderLine = (stage: TLoadStage, tick: number): string =>
  stage === LOAD_STAGE.slow ? 'next.site.loadSlow' : LOADER_LINES[tick % LOADER_LINES.length];

const SiteLoader = ({ stage }: { stage: TLoadStage }) => {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(() => setTick((n) => n + 1), LINE_MS);
    return () => window.clearInterval(timer);
  }, []);
  const line = loaderLine(stage, tick);
  return (
    <div className="hx-site-load" role="status">
      <span className="hx-site-load__mark" aria-hidden="true">
        <span className="hx-site-load__spin" />
        <Icon name="palette" className="hx-site-load__icon" />
      </span>
      <span key={line} className="hx-site-load__text">
        {t(line)}
      </span>
    </div>
  );
};

/**
 * The board as it was last saved, while the editing frame opens: the person sees the design at once and reads it; a
 * thin line says the editing is on its way. It cannot be clicked: the picks belong to the frame that comes.
 * The copy shows only once its runtime says it is drawn (tk:drawn): until then it is the bare template of the board
 * (`{{ c.title }}`, no styles), never to be seen. A copy that never says so stays unseen, the loader stands.
 * The line appears only when the wait is not quick (its css delay).
 */
export const SiteHeld = ({ src, title }: { src: string; title: string }) => {
  const frame = useRef<HTMLIFrameElement>(null);
  /** The address whose copy said it is drawn: a new address is unseen again until its own word. */
  const [drawnFor, setDrawnFor] = useState<string | null>(null);
  const drawn = drawnFor === src;
  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const win = frame.current?.contentWindow;
      if (!win || event.source !== win) return;
      if (readBridgeMessage(event.data)?.k === 'drawn') setDrawnFor(src);
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [src]);
  return (
    <div className="hx-site-held" role="status" data-drawn={drawn}>
      {!drawn && <SiteLoader stage={LOAD_STAGE.opening} />}
      <iframe
        ref={frame}
        className="hx-site-held__frame"
        src={src}
        title={title}
        tabIndex={-1}
        aria-hidden="true"
      />
      {drawn && (
        <span className="hx-site-held__line">
          <span className="hx-site-held__spin" aria-hidden="true" />
          {t('next.site.loadEdit')}
        </span>
      )}
    </div>
  );
};

export default SiteLoader;
