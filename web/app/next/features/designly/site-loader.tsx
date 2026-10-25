// The wait for a design's page: in the middle of the screen where the page will be, the design's icon dimmed,
// a spinner round it and one short line that changes while the wait goes on («Загружаем дизайн…», «Готовим страницы…»).
// A long wait says so instead. Swap (ui/swap.tsx) shows it only after its usual delay and crossfades it with the page;
// it fades in itself, so a quick load never flashes it.
import { useEffect, useState } from 'react';
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

export default SiteLoader;
