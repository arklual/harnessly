import { useEffect, useRef, useState } from 'react';
import dayjs from 'dayjs';
import 'dayjs/locale/ru';
import { ArrowLeft, Check, GitCompareArrows, History, X } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { blockQuote, foldDiff } from '@/lib/doc-versions';
import { DOC_DIFF_KIND } from '@/types/doc-threads';
import type { IDocDiffResponse, IDocVersion } from '@/types/doc-threads';
import { locale, t } from '@/lib/i18n';

const when = (iso: string): string => dayjs(iso).locale(locale()).format('D MMM, HH:mm');
const ago = (iso: string): string => dayjs(iso).locale(locale()).fromNow();

interface IVersionMenuProps {
  versions: IDocVersion[] | null;
  error: string | null;
  /** The version on screen: the one picked in the address, else the current one. */
  shown: IDocVersion | null;
  onPick: (version: IDocVersion) => void;
  onCompare: (version: IDocVersion) => void;
}

/** «Version N · time» in the bar, and the list of every publish of the document under it (no native select). */
export const VersionMenu = ({ versions, error, shown, onPick, onCompare }: IVersionMenuProps) => {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return undefined;
    const onPress = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onPress);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onPress);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  if (!versions && !error) return null;

  return (
    <div className="td-versions" ref={rootRef}>
      <button
        type="button"
        className="td-version-btn"
        data-old={(shown && !shown.current) || undefined}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <History aria-hidden="true" />
        <span>{shown ? t('docs.docVersions.version', { n: shown.n }) : t('docs.docVersions.versions')}</span>
        {shown && <span className="td-version-ago">· {ago(shown.createdAt)}</span>}
      </button>
      {open && (
        <div className="td-version-list" role="menu" aria-label={t('docs.docVersions.documentVersions')}>
          {error && (
            <p className="td-version-error" role="alert">
              {error}
            </p>
          )}
          {versions?.map((version) => {
            const isShown = version.sha === shown?.sha;
            return (
              <div key={version.sha} className={cx('td-version-row', isShown && 'is-shown')}>
                <button
                  type="button"
                  role="menuitem"
                  className="td-version-pick"
                  onClick={() => {
                    setOpen(false);
                    onPick(version);
                  }}
                >
                  <span className="td-version-n">{version.n}</span>
                  <span className="td-version-body">
                    <span className="td-version-head">
                      <b>{when(version.createdAt)}</b>
                      {version.current && <em>{t('docs.docVersions.current')}</em>}
                    </span>
                    <span className="td-version-meta">
                      {version.author ?? t('docs.docVersions.noAuthor')}
                    </span>
                    {version.note && <span className="td-version-note">{version.note}</span>}
                  </span>
                  {isShown && <Check className="td-version-check" aria-label={t('docs.docVersions.shown')} />}
                </button>
                {!isShown && shown && (
                  <button
                    type="button"
                    className="td-icon"
                    title={t('docs.docVersions.compareWith', { n: shown.n })}
                    aria-label={t('docs.docVersions.compareWith', { n: shown.n })}
                    onClick={() => {
                      setOpen(false);
                      onCompare(version);
                    }}
                  >
                    <GitCompareArrows />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};

interface IVersionNoticeProps {
  version: IDocVersion;
  hasPrevious: boolean;
  onBack: () => void;
  onDiff: () => void;
}

/** A thin bar over an old version: what it is, the way back and what changed. */
export const VersionNotice = ({ version, hasPrevious, onBack, onDiff }: IVersionNoticeProps) => (
  <div className="td-version-notice" role="note">
    <span>{t('docs.docVersions.oldVersion', { n: version.n, date: when(version.createdAt) })}</span>
    <div className="td-version-notice-actions">
      <button type="button" onClick={onBack}>
        <ArrowLeft aria-hidden="true" />
        {t('docs.docVersions.toCurrent')}
      </button>
      {hasPrevious && (
        <button type="button" onClick={onDiff}>
          <GitCompareArrows aria-hidden="true" />
          {t('docs.docVersions.whatChanged')}
        </button>
      )}
    </div>
  </div>
);

export type TDiffState =
  { status: 'loading' } | { status: 'error'; message: string } | { status: 'ok'; diff: IDocDiffResponse };

interface IDiffPanelProps {
  from: IDocVersion;
  to: IDocVersion;
  state: TDiffState;
  /** The version on screen: a block can be found in the page only when its text is in that version. */
  shown: IDocVersion | null;
  onFind: (text: string) => void;
  onClose: () => void;
}

/** The text that changed between two versions: added in green, removed in red, unchanged runs folded. */
export const DiffPanel = ({ from, to, state, shown, onFind, onClose }: IDiffPanelProps) => {
  const view = state.status === 'ok' ? foldDiff(state.diff.blocks) : null;
  return (
    <aside className="td-rail td-diff" aria-label={t('docs.docVersions.whatChanged')}>
      <div className="td-diff-head">
        <div className="td-diff-title">
          <b>{t('docs.docVersions.whatChanged')}</b>
          <span>{t('docs.docVersions.fromTo', { from: from.n, to: to.n })}</span>
        </div>
        {state.status === 'ok' && (
          <span
            className="td-diff-stats"
            aria-label={t('docs.docVersions.stats', {
              added: state.diff.stats.added,
              removed: state.diff.stats.removed,
            })}
          >
            <i className="is-added">+{state.diff.stats.added}</i>
            <i className="is-removed">−{state.diff.stats.removed}</i>
          </span>
        )}
        <button type="button" className="td-icon" title={t('docs.docVersions.close')} onClick={onClose}>
          <X />
        </button>
      </div>
      <div className="td-diff-list">
        {state.status === 'loading' && <p className="td-rail-empty">{t('docs.docVersions.comparing')}</p>}
        {state.status === 'error' && (
          <p className="td-rail-empty" role="alert">
            {state.message}
          </p>
        )}
        {view?.empty && <p className="td-rail-empty">{t('docs.docVersions.same')}</p>}
        {view?.rows.map((row, i) => {
          if (row.kind === DOC_DIFF_KIND.same)
            return (
              <p key={i} className="td-diff-fold">
                {t('docs.docVersions.unchanged', { count: row.hidden })}
              </p>
            );
          const findable = row.kind === DOC_DIFF_KIND.added ? shown?.sha === to.sha : shown?.sha === from.sha;
          return (
            <button
              key={i}
              type="button"
              className={cx('td-diff-block', row.kind === DOC_DIFF_KIND.added ? 'is-added' : 'is-removed')}
              disabled={!findable}
              title={findable ? t('docs.docVersions.showInDocument') : t('docs.docVersions.notInShown')}
              onClick={() => onFind(blockQuote(row.text))}
            >
              <span className="td-diff-mark" aria-hidden="true">
                {row.kind === DOC_DIFF_KIND.added ? '+' : '−'}
              </span>
              <span className="td-diff-text">{row.text}</span>
            </button>
          );
        })}
      </div>
    </aside>
  );
};
