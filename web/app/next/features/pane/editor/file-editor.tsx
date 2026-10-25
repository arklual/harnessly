// A text file as an editor (.hx-fed): CodeMirror over the file's text with an IDE's status row — the language, the
// place of the cursor, whether the text differs from the disk. Markdown also reads as a page («Просмотр»). The editor
// keeps its own text; the one who opened it hears whether it is changed and receives the unsaved text when it leaves.
import { EditorState, type Text } from '@codemirror/state';
import { EditorView, type ViewUpdate } from '@codemirror/view';
import { lazy, Suspense, useEffect, useEffectEvent, useRef, useState } from 'react';
import { WordedError } from '@/next/core/net/worded';
import { extensions, language } from '@/next/features/pane/editor/setup';
import { isMarkdown, isPlain, languageLabel, loadLanguage } from '@/next/features/pane/editor/language';
import type { FileContent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import Segmented from '@/next/ui/segmented';

const MdPreview = lazy(() => import('@/next/features/pane/editor/md-preview'));

const EXIT_MS = 200;

/** What the editor opens with, and what it hands back when it leaves unsaved: the text, the disk's text it differs from,
 *  and the revision of that disk text. */
export interface IDraft {
  text: string;
  base: string;
  revision: string;
}

export interface IFileEditorHandle {
  /** Saves; true when the disk has the text afterwards. */
  save: () => Promise<boolean>;
}

export interface IFileEditorProps {
  path: string;
  initial: IDraft;
  /** Resolves with the file as the machine has it after the save. `over`: over whatever the disk has now. */
  onSave: (text: string, revision: string, over: boolean) => Promise<FileContent>;
  /** Drop the edits and open the disk's text. */
  onReload: () => void;
  /** The editor goes: its unsaved text, or null. */
  onLeave: (draft: IDraft | null) => void;
  onState: (state: { dirty: boolean; saving: boolean }) => void;
  /** Hands the save to whoever shows the editor's button; null when the editor goes. */
  onHandle: (handle: IFileEditorHandle | null) => void;
}

interface ILive {
  view: EditorView;
  /** The text the disk has, as the editor last knew it. */
  saved: Text;
  revision: string;
}

interface INotice {
  kind: 'conflict' | 'error';
  text: string;
}

const MODE = { edit: 'edit', preview: 'preview' } as const;
type TMode = (typeof MODE)[keyof typeof MODE];

const stateOf = (saving: boolean, dirty: boolean) => {
  if (saving) return 'saving';
  return dirty ? 'dirty' : 'saved';
};

const FileEditor = ({ path, initial, onSave, onReload, onLeave, onState, onHandle }: IFileEditorProps) => {
  const host = useRef<HTMLDivElement>(null);
  const live = useRef<ILive | null>(null);
  const busy = useRef(false);
  const [start] = useState(initial);
  const markdown = isMarkdown(path);
  const [dirty, setDirty] = useState(start.text !== start.base);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<INotice | null>(null);
  const [mode, setMode] = useState<TMode>(MODE.edit);
  const [page, setPage] = useState('');
  const [place, setPlace] = useState({ line: 1, col: 1, selected: 0 });
  const shownNotice = usePresence(notice, EXIT_MS);
  const reading = usePresentFlag(mode === MODE.preview, EXIT_MS);

  const track = useEffectEvent((update: ViewUpdate) => {
    const l = live.current;
    if (update.docChanged && l) setDirty(!update.state.doc.eq(l.saved));
    if (!update.docChanged && !update.selectionSet) return;
    const head = update.state.selection.main;
    const line = update.state.doc.lineAt(head.head);
    const selected = update.state.selection.ranges.reduce((n, r) => n + r.to - r.from, 0);
    setPlace({ line: line.number, col: head.head - line.from + 1, selected });
  });
  const leave = useEffectEvent((draft: IDraft | null) => onLeave(draft));

  useEffect(() => {
    const parent = host.current;
    if (!parent) return;
    const eol = start.text.includes('\r\n') ? '\r\n' : '\n';
    const state = EditorState.create({
      doc: start.text,
      extensions: extensions({
        eol,
        text: start.text,
        markdown,
        wrap: markdown || isPlain(path),
        onUpdate: track,
      }),
    });
    const view = new EditorView({ state, parent });
    const entry: ILive = { view, saved: state.toText(start.base), revision: start.revision };
    live.current = entry;
    let alive = true;
    void loadLanguage(path).then((support) => {
      if (alive && support) view.dispatch({ effects: language.reconfigure(support) });
    });
    return () => {
      alive = false;
      const unsaved = !view.state.doc.eq(entry.saved);
      const draft = unsaved
        ? {
            text: view.state.sliceDoc(0),
            base: entry.saved.sliceString(0, entry.saved.length, view.state.lineBreak),
            revision: entry.revision,
          }
        : null;
      view.destroy();
      live.current = null;
      leave(draft);
    };
  }, [path, start, markdown]);

  const report = useEffectEvent(() => onState({ dirty, saving }));
  useEffect(() => report(), [dirty, saving]);

  const save = async (over = false): Promise<boolean> => {
    const l = live.current;
    if (!l || busy.current) return false;
    const doc = l.view.state.doc;
    if (!over && doc.eq(l.saved)) return true;
    busy.current = true;
    setSaving(true);
    setNotice(null);
    try {
      const file = await onSave(l.view.state.sliceDoc(0), l.revision, over);
      l.saved = doc;
      l.revision = file.revision;
      setDirty(!l.view.state.doc.eq(doc));
      return true;
    } catch (failure) {
      const conflict = failure instanceof WordedError && failure.code === 'conflict';
      setNotice({
        kind: conflict ? 'conflict' : 'error',
        text: failure instanceof Error ? failure.message : t('next.failure.http'),
      });
      return false;
    } finally {
      busy.current = false;
      setSaving(false);
    }
  };
  const handOver = useEffectEvent((on: boolean) => onHandle(on ? { save: () => save() } : null));
  useEffect(() => {
    handOver(true);
    return () => handOver(false);
  }, []);

  // Ctrl/Cmd+S saves from the editor or from the pane around it, not from the rest of the page.
  const press = useEffectEvent((event: KeyboardEvent) => {
    if (!(event.ctrlKey || event.metaKey) || event.altKey || event.key.toLowerCase() !== 's') return;
    const target = event.target as Element | null;
    if (target && target !== document.body && !target.closest('.hx-fed')) return;
    event.preventDefault();
    void save();
  });
  useEffect(() => {
    document.addEventListener('keydown', press);
    return () => document.removeEventListener('keydown', press);
  }, []);

  const changeMode = (next: TMode) => {
    if (next === MODE.preview) setPage(live.current?.view.state.sliceDoc(0) ?? '');
    setMode(next);
    if (next === MODE.edit) live.current?.view.focus();
  };

  const stateKey = stateOf(saving, dirty);
  return (
    <div className="hx-fed" data-kind={markdown ? 'markdown' : 'code'}>
      {markdown && (
        <div className="hx-fed__bar">
          <Segmented
            label={t('next.editor.mode')}
            value={mode}
            options={[
              { value: MODE.edit, label: t('next.editor.edit') },
              { value: MODE.preview, label: t('next.editor.preview') },
            ]}
            onChange={changeMode}
          />
        </div>
      )}
      {shownNotice.value && (
        <div
          className="hx-fed__notice"
          data-kind={shownNotice.value.kind}
          data-state={shownNotice.state}
          role="alert"
        >
          <Icon name="triangle-alert" size="sm" />
          <span className="hx-fed__notice-text">{shownNotice.value.text}</span>
          {shownNotice.value.kind === 'conflict' && (
            <>
              <button type="button" className="hx-btn" data-size="sm" onClick={onReload}>
                <span className="hx-btn__text">{t('next.editor.reload')}</span>
              </button>
              <button type="button" className="hx-btn" data-size="sm" onClick={() => void save(true)}>
                <span className="hx-btn__text">{t('next.editor.overwrite')}</span>
              </button>
            </>
          )}
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.editor.dismiss')}
            onClick={() => setNotice(null)}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
      )}
      <div className="hx-fed__main">
        <div ref={host} className="hx-fed__host" />
        {reading.mounted && (
          <div className="hx-fed__page" data-state={reading.state}>
            <Suspense fallback={null}>
              <MdPreview md={page} />
            </Suspense>
          </div>
        )}
      </div>
      <div className="hx-fed__foot">
        <span className="hx-fed__lang">{languageLabel(path)}</span>
        <span>{t('next.editor.cursor', { line: place.line, col: place.col })}</span>
        {place.selected > 0 && <span>{t('next.editor.selected', { n: place.selected })}</span>}
        <span className="hx-spacer" />
        <span className="hx-fed__state" data-state={stateKey} role="status">
          {t(`next.editor.${stateKey}`)}
        </span>
      </div>
    </div>
  );
};

export default FileEditor;
