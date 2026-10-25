// A ```mermaid answer drawn as a diagram. The renderer is a chunk of its own, fetched when the first diagram shows;
// until it is there the box is held at a diagram's height, and a source it cannot draw stays the code block it was.
import { useEffect, useState, type ReactNode } from 'react';
import RichView from '@/next/features/chat/rich-view';
import useDark from '@/next/features/chat/use-dark';
import Icon from '@/next/ui/icon';
import Spinner from '@/next/ui/spinner';
import { t } from '@/next/i18n';

type TMermaid = typeof import('mermaid').default;

let loading: Promise<TMermaid> | null = null;
const loadMermaid = (): Promise<TMermaid> => {
  loading ??= import('mermaid').then((m) => m.default);
  return loading;
};

// The renderer keeps one configuration, so drawings go one after another, each with the theme of its own turn.
let queue: Promise<unknown> = Promise.resolve();
let drawn = 0;

const draw = (source: string, dark: boolean): Promise<string> => {
  const job = queue.then(async () => {
    const mermaid = await loadMermaid();
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: dark ? 'dark' : 'default',
      fontFamily: 'inherit',
    });
    const id = `hx-mmd-${++drawn}`;
    try {
      return (await mermaid.render(id, source)).svg;
    } finally {
      document.getElementById(`d${id}`)?.remove();
    }
  });
  queue = job.catch(() => undefined);
  return job;
};

type TDrawing = { state: 'loading' } | { state: 'failed' } | { state: 'ready'; svg: string };

interface IMermaidBlockProps {
  source: string;
  /** What shows when the diagram cannot be drawn: the code block of the source. */
  fallback: ReactNode;
}

const MermaidBlock = ({ source, fallback }: IMermaidBlockProps) => {
  const dark = useDark();
  const [drawing, setDrawing] = useState<TDrawing>({ state: 'loading' });
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let current = true;
    draw(source, dark).then(
      (svg) => current && setDrawing({ state: 'ready', svg }),
      () => current && setDrawing({ state: 'failed' }),
    );
    return () => {
      current = false;
    };
  }, [source, dark]);

  if (drawing.state === 'failed') return fallback;
  const copy = () => {
    void navigator.clipboard?.writeText(source).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    });
  };
  return (
    <div className="hx-rich" data-kind="diagram">
      <div className="hx-rich__head">
        <span>{t('next.rich.diagram')}</span>
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-rich__btn"
          aria-label={t('next.code.copy')}
          data-copied={copied || undefined}
          onClick={copy}
        >
          <Icon name={copied ? 'check' : 'copy'} size="sm" />
          <span className="hx-rich__lbl">{copied ? t('next.code.copied') : t('next.code.copy')}</span>
        </button>
        <button
          type="button"
          className="hx-rich__btn"
          aria-label={t('next.rich.expand')}
          disabled={drawing.state !== 'ready'}
          onClick={() => setOpen(true)}
        >
          <Icon name="maximize-2" size="sm" />
        </button>
      </div>
      {drawing.state === 'ready' ? (
        <>
          <div
            className="hx-rich__diagram"
            role="button"
            tabIndex={0}
            aria-label={t('next.rich.expand')}
            onClick={() => setOpen(true)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                setOpen(true);
              }
            }}
            dangerouslySetInnerHTML={{ __html: drawing.svg }}
          />
          <RichView open={open} kind="diagram" title={t('next.rich.diagram')} onClose={() => setOpen(false)}>
            <div className="hx-rich__big" dangerouslySetInnerHTML={{ __html: drawing.svg }} />
          </RichView>
        </>
      ) : (
        <div className="hx-rich__wait" data-busy="true">
          <Spinner />
        </div>
      )}
    </div>
  );
};

export default MermaidBlock;
