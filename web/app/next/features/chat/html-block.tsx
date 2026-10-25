// An ```html answer: the page the agent wrote, in a sandboxed frame (html-doc.ts) that grows to its content, with the
// source one toggle away and the page full size over the feed.
import { useEffect, useMemo, useState } from 'react';
import { frameDocument, FRAME_SANDBOX, isFrameHeight, newNonce } from '@/next/features/chat/html-doc';
import RichView from '@/next/features/chat/rich-view';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

type TMode = 'preview' | 'code';

const MIN_HEIGHT = 48;
const MAX_HEIGHT = 640;

interface IPageFrameProps {
  html: string;
  fit: boolean;
  height?: number;
  onHeight?: (height: number) => void;
}

/** The frame itself. Its document is rebuilt only when the source changes. */
const PageFrame = ({ html, fit, height, onHeight }: IPageFrameProps) => {
  // Random per frame: another frame (or the agent's page) cannot guess it to forge a height.
  const [key] = useState(() => crypto.randomUUID());
  const [frame, setFrame] = useState<HTMLIFrameElement | null>(null);
  const doc = useMemo(() => frameDocument(html, { key, nonce: newNonce(), fit }), [html, key, fit]);

  useEffect(() => {
    if (!fit || !frame || !onHeight) return;
    const onMessage = (event: MessageEvent) => {
      if (event.source === frame.contentWindow && isFrameHeight(event.data, key)) onHeight(event.data.height);
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [fit, frame, key, onHeight]);

  return (
    <iframe
      ref={setFrame}
      className="hx-rich__frame"
      title={t('next.rich.page')}
      sandbox={FRAME_SANDBOX}
      referrerPolicy="no-referrer"
      srcDoc={doc}
      data-fit={fit || undefined}
      height={fit ? height : undefined}
    />
  );
};

const HtmlBlock = ({ source }: { source: string }) => {
  const [mode, setMode] = useState<TMode>('preview');
  const [height, setHeight] = useState(MIN_HEIGHT * 3);
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const code = source.replace(/\n$/, '');
  const copy = () => {
    void navigator.clipboard?.writeText(source).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    });
  };
  const fitted = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, height));
  return (
    <div className="hx-rich" data-kind="page">
      <div className="hx-rich__head">
        <Segmented<TMode>
          label={t('next.rich.page')}
          value={mode}
          options={[
            { value: 'preview', label: t('next.rich.preview') },
            { value: 'code', label: t('next.rich.code') },
          ]}
          onChange={setMode}
        />
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
          onClick={() => setOpen(true)}
        >
          <Icon name="maximize-2" size="sm" />
        </button>
      </div>
      {mode === 'preview' ? (
        <PageFrame html={source} fit height={fitted} onHeight={setHeight} />
      ) : (
        <pre className="hx-rich__code">{code}</pre>
      )}
      <RichView open={open} kind="page" title={t('next.rich.page')} onClose={() => setOpen(false)}>
        <PageFrame html={source} fit={false} />
      </RichView>
    </div>
  );
};

export default HtmlBlock;
