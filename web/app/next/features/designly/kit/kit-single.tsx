// A template's layout alone and large (Figma Slides' single view): the live page over its picture, ← → walk the
// template in its order, Esc goes back to the grid. It leaves with an animation like everything that closes.
import { useEffect, useMemo, useState } from 'react';
import type { IKitLayout, IKitTemplate } from '@/next/features/designly/kit/kit-api';
import { templateLayouts } from '@/next/features/designly/kit/kit-world';
import { RoundButton } from '@/next/ui/button';
import { usePresence, PRESENCE } from '@/next/ui/presence';
import { t } from '@/next/i18n';

interface IKitSingleProps {
  template: IKitTemplate | null;
  layout: string | null;
  fileUrl: (rel: string) => string;
  /** The layout's live page, null while the preview slot opens. */
  liveSrc: (layout: IKitLayout) => string | null;
  onMove: (node: string) => void;
  onClose: () => void;
}

const EXIT_MS = 180;

const KitSingle = ({ template, layout, fileUrl, liveSrc, onMove, onClose }: IKitSingleProps) => {
  const open = useMemo(() => (template && layout ? { template, layout } : null), [template, layout]);
  const shown = usePresence(open, EXIT_MS);
  const [ready, setReady] = useState<string | null>(null);
  const list = useMemo(() => (shown.value ? templateLayouts(shown.value.template) : []), [shown.value]);
  const i = list.findIndex((l) => l.id === shown.value?.layout);
  const cur = list[i];

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
        const next = list[i + (e.key === 'ArrowRight' ? 1 : -1)];
        if (next) {
          e.preventDefault();
          onMove(next.node);
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, list, i, onMove, onClose]);

  if (!shown.value || !cur) return null;
  const src = liveSrc(cur);
  const aspect = shown.value.template.aspect;
  const prev = list[i - 1];
  const next = list[i + 1];
  return (
    <div
      className="hx-kit-single"
      data-state={shown.state === PRESENCE.closing ? 'closing' : 'open'}
      role="dialog"
      aria-label={cur.name}
    >
      <div
        className="hx-kit-single__stage"
        ref={(el) => {
          el?.style.setProperty('--ar', `${aspect[0]} / ${aspect[1]}`);
          el?.style.setProperty('--ar-n', String(aspect[0] / aspect[1]));
        }}
      >
        {cur.thumb && <img className="hx-kit-single__shot" src={fileUrl(cur.thumb)} alt="" />}
        {src && (
          <iframe
            key={src}
            className="hx-kit-single__live"
            data-ready={ready === src || undefined}
            src={src}
            title={cur.name}
            onLoad={() => window.setTimeout(() => setReady(src), 500)}
          />
        )}
      </div>
      <div className="hx-kit-single__bar">
        <RoundButton
          icon="arrow-left"
          label={t('next.kit.single.prev')}
          keys={['←']}
          disabled={!prev}
          onClick={() => prev && onMove(prev.node)}
        />
        <span className="hx-kit-single__name">{cur.name}</span>
        <span className="hx-kit-single__pos">
          {i + 1} / {list.length}
        </span>
        <RoundButton
          icon="arrow-right"
          label={t('next.kit.single.next')}
          keys={['→']}
          disabled={!next}
          onClick={() => next && onMove(next.node)}
        />
        <RoundButton
          icon="layout-grid"
          label={t('next.kit.single.grid')}
          keys={['Esc']}
          variant="ghost"
          onClick={onClose}
        />
      </div>
    </div>
  );
};

export default KitSingle;
