import { useCallback, useEffect, useRef, useState } from 'react';
import { Pencil, X } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { DocThreadsError, fetchDocSource, saveDocBlock, saveDocSource } from '@/lib/doc-threads-client';
import { findBlock, mdBlocks, replaceBlock, type IShownBlock } from '@/lib/doc-md-blocks';
import { t } from '@/lib/i18n';
import { DOC_EDIT_MODE, type TDocEditMode, type TDocParentMessage } from '@/types/doc-threads';

/** Keep in step with the exit animation of `.td-card` in hl-docs.css. */
const EXIT_MS = 120;

export const EDIT_SCOPE = { block: 'block', all: 'all', inline: 'inline' } as const;
type TEditScope = (typeof EDIT_SCOPE)[keyof typeof EDIT_SCOPE];

const PHASE = {
  loading: 'loading',
  ready: 'ready',
  saving: 'saving',
  conflict: 'conflict',
  /** A block that is markup or a widget: it is not offered, the card says why. */
  fixed: 'fixed',
  /** The pencil of a page edited in place: the card tells how. */
  hint: 'hint',
} as const;
type TPhase = (typeof PHASE)[keyof typeof PHASE];

interface IEditing {
  phase: TPhase;
  scope: TEditScope;
  /** The page's block asked for (Markdown); null for the whole text. */
  shown: IShownBlock | null;
  /** The whole source the edit was started on, its version and the edited part of it; null while it is being read. */
  source: { markdown: string; version: number; start: number; end: number } | null;
  draft: string;
  /** Asked for a block the source does not have for sure: the whole text is open instead. */
  missed: boolean;
  /** The block of the page edited in place. */
  index: number | null;
  error: string | null;
}

/** What the current version allows, as the gateway said it; null until it has. */
export interface IEditInfo {
  mode: TDocEditMode;
  version: number;
}

const messageOf = (err: unknown, fallback: string): string =>
  err instanceof Error && err.message ? err.message : fallback;

const blank = (scope: TEditScope, phase: TPhase): IEditing => ({
  phase,
  scope,
  shown: null,
  source: null,
  draft: '',
  missed: false,
  index: null,
  error: null,
});

export interface IDocEditor {
  editing: IEditing | null;
  leaving: boolean;
  /** The pencil: the whole Markdown text, or how a page is edited in place. */
  openAll: () => void;
  /** A double click on a block of the page. */
  openBlock: (block: IShownBlock & { index: number | null }) => void;
  /** The frame finished the block being edited in place. */
  commit: (edit: { index: number; html: string; before: string; changed: boolean }) => void;
  /** The frame dropped the edit itself (Esc in the block). */
  dropped: () => void;
  cancel: () => void;
  setDraft: (draft: string) => void;
  save: () => void;
  again: () => void;
}

/**
 * Editing a document in place. A version with a page of its own is edited block by block right in the page: only that
 * block's content changes, its markup and the widgets around stay as published; a version made of Markdown is edited as
 * Markdown. A save is a new version on top of the version the edit started from: the gateway refuses it when somebody
 * published in between, and the person is told so. Nothing on the page changes until the gateway says the version is made.
 */
export const useDocEditor = (
  slug: string,
  info: IEditInfo | null,
  postToFrame: (message: TDocParentMessage) => void,
  onSaved: () => void,
  onStale: () => void,
): IDocEditor => {
  const [editing, setEditing] = useState<IEditing | null>(null);
  const [leaving, setLeaving] = useState(false);
  const token = useRef(0);
  const leavingRef = useRef(false);

  const close = useCallback(() => {
    if (leavingRef.current) return;
    token.current += 1;
    leavingRef.current = true;
    setLeaving(true);
    window.setTimeout(() => {
      leavingRef.current = false;
      setLeaving(false);
      setEditing(null);
    }, EXIT_MS);
  }, []);

  const openMarkdown = useCallback(
    (shown: IShownBlock | null) => {
      if (leavingRef.current) return;
      const mine = token.current + 1;
      token.current = mine;
      setEditing({ ...blank(shown ? EDIT_SCOPE.block : EDIT_SCOPE.all, PHASE.loading), shown });
      fetchDocSource(slug).then(
        (source) => {
          if (token.current !== mine) return;
          if (!source.canEdit || source.mode !== DOC_EDIT_MODE.markdown || source.markdown === null) {
            setEditing(
              (current) =>
                current && { ...current, phase: PHASE.conflict, error: t('docs.docEditor.unavailable') },
            );
            return;
          }
          const markdown = source.markdown;
          const block = shown ? findBlock(mdBlocks(markdown), shown) : null;
          const range = block ?? { start: 0, end: markdown.length };
          setEditing({
            ...blank(block ? EDIT_SCOPE.block : EDIT_SCOPE.all, PHASE.ready),
            shown,
            source: { markdown, version: source.version, ...range },
            draft: markdown.slice(range.start, range.end),
            missed: Boolean(shown && !block),
          });
        },
        (err: unknown) => {
          if (token.current !== mine) return;
          setEditing(
            (current) =>
              current && {
                ...current,
                phase: PHASE.conflict,
                error: messageOf(err, t('docs.docEditor.errorLoad')),
              },
          );
        },
      );
    },
    [slug],
  );

  const openAll = useCallback(() => {
    if (!info || leavingRef.current) return;
    if (info.mode === DOC_EDIT_MODE.markdown) openMarkdown(null);
    else {
      token.current += 1;
      setEditing(blank(EDIT_SCOPE.inline, PHASE.hint));
    }
  }, [info, openMarkdown]);

  const openBlock = useCallback(
    (block: IShownBlock & { index: number | null }) => {
      if (!info || leavingRef.current) return;
      if (info.mode === DOC_EDIT_MODE.markdown) {
        // A Markdown page has no widgets of its own: a press outside its text opens nothing.
        if (block.text.trim()) openMarkdown(block);
        return;
      }
      token.current += 1;
      if (block.index === null) {
        setEditing(blank(EDIT_SCOPE.inline, PHASE.fixed));
        return;
      }
      setEditing({ ...blank(EDIT_SCOPE.inline, PHASE.ready), index: block.index });
      postToFrame({ type: 'doc:edit-start', index: block.index });
    },
    [info, openMarkdown, postToFrame],
  );

  const commit = useCallback(
    (edit: { index: number; html: string; before: string; changed: boolean }) => {
      if (!info || !editing || editing.index !== edit.index || editing.phase === PHASE.saving) return;
      if (!edit.changed) {
        close();
        return;
      }
      const mine = token.current;
      setEditing({ ...editing, phase: PHASE.saving, error: null });
      saveDocBlock(slug, {
        index: edit.index,
        before: edit.before,
        html: edit.html,
        baseVersion: info.version,
      }).then(
        () => {
          if (token.current !== mine) return;
          onSaved();
          close();
        },
        (err: unknown) => {
          if (token.current !== mine) return;
          const stale = err instanceof DocThreadsError && err.status === 409;
          if (stale) {
            // The page on screen is not the version the save was made against: back to it as published.
            postToFrame({ type: 'doc:edit-revert' });
            onStale();
          } else postToFrame({ type: 'doc:edit-start', index: edit.index });
          setEditing((now) =>
            now
              ? {
                  ...now,
                  phase: stale ? PHASE.conflict : PHASE.ready,
                  error: stale
                    ? t('docs.docEditor.inlineConflict', { reason: messageOf(err, '') })
                    : messageOf(err, t('docs.docEditor.errorSave')),
                }
              : now,
          );
        },
      );
    },
    [info, editing, slug, close, onSaved, onStale, postToFrame],
  );

  const setDraft = useCallback((draft: string) => {
    setEditing((current) => (current && current.phase === PHASE.ready ? { ...current, draft } : current));
  }, []);

  const save = useCallback(() => {
    const current = editing;
    if (!current || current.phase !== PHASE.ready) return;
    if (current.scope === EDIT_SCOPE.inline) {
      postToFrame({ type: 'doc:edit-finish' });
      return;
    }
    if (!current.source) return;
    const { markdown, version } = current.source;
    const next = replaceBlock(markdown, current.source, current.draft);
    if (next === markdown) {
      close();
      return;
    }
    const mine = token.current;
    setEditing({ ...current, phase: PHASE.saving, error: null });
    saveDocSource(slug, next, version).then(
      () => {
        if (token.current !== mine) return;
        onSaved();
        close();
      },
      (err: unknown) => {
        if (token.current !== mine) return;
        const stale = err instanceof DocThreadsError && err.code === 'version_conflict';
        if (stale) onStale();
        setEditing((now) =>
          now
            ? {
                ...now,
                phase: stale ? PHASE.conflict : PHASE.ready,
                error: stale
                  ? t('docs.docEditor.conflict', { reason: messageOf(err, '') })
                  : messageOf(err, t('docs.docEditor.errorSave')),
              }
            : now,
        );
      },
    );
  }, [editing, slug, close, onSaved, onStale, postToFrame]);

  const cancel = useCallback(() => {
    if (editing?.scope === EDIT_SCOPE.inline && editing.index !== null)
      postToFrame({ type: 'doc:edit-revert' });
    close();
  }, [editing, close, postToFrame]);

  const again = useCallback(() => openMarkdown(editing?.shown ?? null), [editing, openMarkdown]);

  // Another document in the same viewer: an edit of the old one is dropped, and its answers are not waited for.
  const [shownSlug, setShownSlug] = useState(slug);
  if (shownSlug !== slug) {
    setShownSlug(slug);
    setEditing(null);
  }
  useEffect(
    () => () => {
      token.current += 1;
    },
    [slug],
  );

  return { editing, leaving, openAll, openBlock, commit, dropped: close, cancel, setDraft, save, again };
};

interface IDocEditorCardProps {
  editor: IDocEditor;
}

const NOTE: Partial<Record<TPhase, string>> = {
  [PHASE.fixed]: 'docs.docEditor.fixed',
  [PHASE.hint]: 'docs.docEditor.inlineHint',
};

/**
 * The card of an edit. Markdown is edited in it: one block by the bottom of the document, the whole text over all of it.
 * A page edited in place is typed into right on the page; the card then only says how to finish, and why a block is not
 * offered.
 */
export const DocEditorCard = ({ editor }: IDocEditorCardProps) => {
  const { editing, leaving, cancel, setDraft, save, again } = editor;
  const field = useRef<HTMLTextAreaElement | null>(null);
  const ready = editing?.phase === PHASE.ready;

  useEffect(() => {
    if (ready) field.current?.focus();
  }, [ready]);

  if (!editing) return null;
  const busy = editing.phase === PHASE.saving;
  const inline = editing.scope === EDIT_SCOPE.inline;
  const note = NOTE[editing.phase];
  const editingInline = inline && editing.index !== null;
  const label = {
    [EDIT_SCOPE.block]: t('docs.docEditor.block'),
    [EDIT_SCOPE.all]: t('docs.docEditor.all'),
    [EDIT_SCOPE.inline]: t('docs.docEditor.inline'),
  }[editing.scope];

  return (
    <div
      className={cx('td-card', 'td-editor', leaving && 'is-leaving')}
      data-scope={editing.scope}
      role="dialog"
      aria-label={t('docs.docEditor.title')}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          if (!busy) cancel();
        } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
          event.preventDefault();
          save();
        }
      }}
    >
      <div className="td-editor-head">
        <Pencil aria-hidden="true" />
        <span className="td-editor-title">{label}</span>
        {!inline && <span className="td-editor-hint">{t('docs.docEditor.markdown')}</span>}
        <button
          type="button"
          className="td-icon"
          title={t('docs.docViewer.cancelEsc')}
          disabled={busy}
          onClick={cancel}
        >
          <X />
        </button>
      </div>
      {note && <p className="td-editor-note">{t(note)}</p>}
      {editing.missed && <p className="td-editor-note">{t('docs.docEditor.missed')}</p>}
      {editing.phase === PHASE.loading && (
        <p className="td-editor-loading" role="status">
          {t('docs.docEditor.loading')}
        </p>
      )}
      {!inline && editing.phase !== PHASE.loading && editing.source && (
        <textarea
          ref={field}
          className="td-editor-field"
          value={editing.draft}
          readOnly={!ready}
          spellCheck
          aria-label={label}
          onChange={(event) => setDraft(event.target.value)}
        />
      )}
      {editing.error && (
        <p className="td-editor-error" role="alert">
          {editing.error}
        </p>
      )}
      <div className="td-editor-foot">
        {(editingInline || !inline) && editing.phase !== PHASE.conflict && (
          <span className="td-editor-keys">
            {editingInline ? t('docs.docEditor.inlineKeys') : t('docs.docEditor.keys')}
          </span>
        )}
        <button type="button" className="td-share-action" disabled={busy} onClick={cancel}>
          {editing.phase === PHASE.conflict || note ? t('docs.docViewer.close') : t('docs.docEditor.cancel')}
        </button>
        {editing.phase === PHASE.conflict && editing.source && (
          <button type="button" className="td-share-action" data-primary onClick={again}>
            {t('docs.docEditor.again')}
          </button>
        )}
        {(editingInline || !inline) && editing.phase !== PHASE.conflict && (
          <button
            type="button"
            className="td-share-action"
            data-primary
            disabled={!ready}
            aria-busy={busy || undefined}
            onClick={save}
          >
            {busy ? t('docs.docEditor.saving') : t('docs.docEditor.save')}
          </button>
        )}
      </div>
    </div>
  );
};
