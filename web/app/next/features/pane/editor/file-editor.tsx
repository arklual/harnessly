// A text file as an editor (.hx-fed): CodeMirror over the file's text with an IDE's status row — the language, the
// place of the cursor, whether the text differs from the disk. Markdown also reads as a page («Просмотр»). The editor
// keeps its own text; the one who opened it hears whether it is changed and receives the unsaved text when it leaves.
import { usePhone } from '@/next/ui/use-media';
import { EditorState, type Text } from '@codemirror/state';
import { EditorView, type ViewUpdate } from '@codemirror/view';
import { openSearchPanel } from '@codemirror/search';
import { createPortal } from 'react-dom';
import { lazy, Suspense, useEffect, useEffectEvent, useRef, useState } from 'react';
import { WordedError } from '@/next/core/net/worded';
import { extensions, language } from '@/next/features/pane/editor/setup';
import { askTip, type IAskChoice } from '@/next/features/pane/editor/ask-tip';
import {
  isMarkdown,
  isPlain,
  languageLabel,
  loadLanguage,
  SHOWN_AS,
  shownAs,
  type TShownAs,
} from '@/next/features/pane/editor/language';
import { inlineAssets, type TReadAsset } from '@/next/features/pane/editor/html-assets';
import type { FileContent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import Segmented from '@/next/ui/segmented';

const MdPreview = lazy(() => import('@/next/features/pane/editor/md-preview'));

const JsonTree = lazy(() => import('@/next/features/pane/editor/json-tree'));
const DiffView = lazy(() => import('@/next/features/pane/editor/diff-view'));

/** An html file as the page it makes, from the editor's text (unsaved edits too), with the files it names beside it
 *  put in. Its scripts run in a frame of no origin: they reach neither the app nor its cookies. */
const HtmlPage = ({
  html,
  name,
  path,
  readAsset,
}: {
  html: string;
  name: string;
  path: string;
  readAsset?: TReadAsset;
}) => {
  const [page, setPage] = useState<{ from: string; doc: string } | null>(null);
  useEffect(() => {
    if (!readAsset) return undefined;
    let alive = true;
    inlineAssets(html, path, readAsset).then(
      (doc) => alive && setPage({ from: html, doc }),
      () => alive && setPage({ from: html, doc: html }),
    );
    return () => {
      alive = false;
    };
  }, [html, path, readAsset]);
  const doc = readAsset ? (page?.from === html ? page.doc : null) : html;
  if (doc === null) return <span className="hx-sk hx-fed__wait" />;
  return <iframe className="hx-fed__frame" title={name} sandbox="allow-scripts allow-popups" srcDoc={doc} />;
};

/** An svg file as the picture it draws, from the editor's text; as an image its scripts never run. */
const SvgPicture = ({ svg, name }: { svg: string; name: string }) => (
  <div className="hx-fed__pic">
    <img alt={name} src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`} />
  </div>
);

/** The modes of a file that reads two ways: which one is the text, which the view, and which comes first. */
const MODES_OF: Record<TShownAs, { view: string; edit: string; first: 'edit' | 'preview' }> = {
  markdown: { view: 'next.editor.preview', edit: 'next.editor.edit', first: 'edit' },
  html: { view: 'next.editor.page', edit: 'next.editor.code', first: 'preview' },
  json: { view: 'next.editor.tree', edit: 'next.editor.code', first: 'preview' },
  svg: { view: 'next.editor.picture', edit: 'next.editor.code', first: 'preview' },
};

const EXIT_MS = 200;

/** The bar over a selection: ask, explain, fix. */
const askChoices = (): IAskChoice[] => [
  { label: t('next.editor.ask'), note: null, icon: true },
  { label: t('next.editor.askExplain'), note: t('next.editor.askExplainNote') },
  { label: t('next.editor.askFix'), note: t('next.editor.askFixNote') },
];

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
  /** Reads a file beside this one (relative to its folder): an html page's styles, scripts and pictures. */
  readAsset?: TReadAsset;
  /** Another state of the file to show the editor's text against (git's, another file's); null: no diff. */
  compare?: ICompare | null;
  onCloseCompare?: () => void;
  /** A place to show (a line «Найти в файлах» found): the text there is selected and scrolled to the middle. */
  at?: { line: number; col: number; size: number } | null;
  /** Hands the selection to the chat's field as a quote: the path with the lines, the code, the person's ask. */
  onAsk?: (quote: string, note: string | null) => void;
  /** Where the switch of the view goes (the folder's head row, beside the crumbs); without it, a bar of its own. */
  modeSlot?: HTMLElement | null;
}

/** What the editor's text is shown against: its name over the diff, its text, or why there is none. */
export interface ICompare {
  label: string;
  text: string | null;
  /** Why there is nothing to compare with (not in git, a binary, too large); null while it loads or when it has text. */
  failure: string | null;
}

const LAYOUT = { split: 'split', inline: 'inline' } as const;
type TLayout = (typeof LAYOUT)[keyof typeof LAYOUT];

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

const nameOf = (path: string) => path.slice(path.lastIndexOf('/') + 1);

/** The language word of a markdown code fence: the file's extension, which most renderers know. */
const fenceLanguage = (path: string) => {
  const name = nameOf(path);
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
};

const stateOf = (saving: boolean, dirty: boolean) => {
  if (saving) return 'saving';
  return dirty ? 'dirty' : 'saved';
};

const FileEditor = ({
  path,
  initial,
  onSave,
  onReload,
  onLeave,
  onState,
  onHandle,
  readAsset,
  compare = null,
  onCloseCompare,
  onAsk,
  modeSlot = null,
  at = null,
}: IFileEditorProps) => {
  const host = useRef<HTMLDivElement>(null);
  const live = useRef<ILive | null>(null);
  const busy = useRef(false);
  const [start] = useState(initial);
  const markdown = isMarkdown(path);
  const shown = shownAs(path);
  const modes = shown ? MODES_OF[shown] : null;
  const [dirty, setDirty] = useState(start.text !== start.base);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<INotice | null>(null);
  // A page, a tree or a picture opens as what it makes: that is what it is for; the source is a click away.
  // A file opened at a found line opens as its text: the line is what was asked for.
  const opensAsPage = modes?.first === 'preview' && at === null;
  const [mode, setMode] = useState<TMode>(opensAsPage ? MODE.preview : MODE.edit);
  const [page, setPage] = useState(opensAsPage ? start.text : '');
  const [place, setPlace] = useState({ line: 1, col: 1, selected: 0 });
  const shownNotice = usePresence(notice, EXIT_MS);
  const reading = usePresentFlag(mode === MODE.preview, EXIT_MS);
  // The diff over the editor: its text is the editor's as it was when the diff opened.
  const diff = usePresence(compare, EXIT_MS);
  // Two columns do not fit a phone: there the diff starts inside the text, until the person picks otherwise.
  const phone = usePhone();
  const [picked, setLayout] = useState<TLayout | null>(null);
  const layout = picked ?? (phone ? LAYOUT.inline : LAYOUT.split);
  const [after, setAfter] = useState('');
  const readText = useEffectEvent(() => live.current?.view.state.sliceDoc(0) ?? '');
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the editor's text is read once, as the diff opens
    if (compare) setAfter(readText());
  }, [compare]);

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

  // The selection as a quote for the chat: where it is (path and lines) and the code in a fence of its language.
  const ask = (view: EditorView, note: string | null) => {
    if (!onAsk) return;
    const range = view.state.selection.main;
    if (range.empty) return;
    const from = view.state.doc.lineAt(range.from).number;
    const to = view.state.doc.lineAt(range.to).number;
    const lines = from === to ? `${from}` : `${from}-${to}`;
    const fence = view.state.sliceDoc(range.from, range.to).includes('```') ? '````' : '```';
    const lang = fenceLanguage(path);
    onAsk(`${path}:${lines}\n${fence}${lang}\n${view.state.sliceDoc(range.from, range.to)}\n${fence}`, note);
  };
  const askOf = useEffectEvent(ask);
  // Whether there is a chat to ask: the bar over a selection is part of the editor only then.
  const askable = onAsk !== undefined;

  useEffect(() => {
    const parent = host.current;
    if (!parent) return;
    const eol = start.text.includes('\r\n') ? '\r\n' : '\n';
    const state = EditorState.create({
      doc: start.text,
      extensions: [
        extensions({
          eol,
          text: start.text,
          markdown,
          wrap: markdown || isPlain(path),
          onUpdate: track,
        }),
        ...(askable ? [askTip(askChoices, (on, note) => askOf(on, note))] : []),
      ],
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
  }, [path, start, markdown, askable]);

  // The place asked for: selected, in the middle of the view, with the focus in the text.
  const atLine = at?.line ?? 0;
  const atCol = at?.col ?? 0;
  const atSize = at?.size ?? 0;
  useEffect(() => {
    const view = live.current?.view;
    if (!view || atLine < 1) return;
    const doc = view.state.doc;
    const line = doc.line(Math.min(atLine, doc.lines));
    const from = Math.min(line.from + Math.max(atCol - 1, 0), line.to);
    const to = Math.min(from + atSize, line.to);
    view.dispatch({
      selection: { anchor: from, head: to },
      effects: EditorView.scrollIntoView(from, { y: 'center' }),
    });
    view.focus();
  }, [path, start, atLine, atCol, atSize]);

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
  const modeSwitch = modes && (
    <Segmented
      label={t('next.editor.mode')}
      value={mode}
      options={
        modes.first === 'preview'
          ? [
              { value: MODE.preview, label: t(modes.view) },
              { value: MODE.edit, label: t(modes.edit) },
            ]
          : [
              { value: MODE.edit, label: t(modes.edit) },
              { value: MODE.preview, label: t(modes.view) },
            ]
      }
      onChange={changeMode}
    />
  );
  return (
    <div className="hx-fed" data-kind={shown ?? 'code'}>
      {modes && modeSlot && createPortal(modeSwitch, modeSlot)}
      {modes && !modeSlot && <div className="hx-fed__bar">{modeSwitch}</div>}
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
          <div className="hx-fed__page" data-state={reading.state} data-view={shown ?? undefined}>
            {shown === SHOWN_AS.html && (
              <HtmlPage html={page} name={nameOf(path)} path={path} readAsset={readAsset} />
            )}
            {shown === SHOWN_AS.svg && <SvgPicture svg={page} name={nameOf(path)} />}
            {(shown === SHOWN_AS.markdown || shown === SHOWN_AS.json) && (
              <Suspense fallback={null}>
                {shown === SHOWN_AS.markdown ? <MdPreview md={page} /> : <JsonTree text={page} />}
              </Suspense>
            )}
          </div>
        )}
        {diff.value && (
          <div className="hx-fed__diff" data-state={diff.state}>
            <div className="hx-fed__diffbar">
              <Icon name="git-compare" size="sm" />
              <span className="hx-fed__difflabel">{diff.value.label}</span>
              <span className="hx-spacer" />
              <Segmented
                label={t('next.editor.diffLayout')}
                value={layout}
                options={[
                  { value: LAYOUT.split, label: t('next.editor.diffSplit') },
                  { value: LAYOUT.inline, label: t('next.editor.diffInline') },
                ]}
                onChange={setLayout}
              />
              <button
                type="button"
                className="hx-head-btn"
                aria-label={t('next.editor.diffClose')}
                onClick={onCloseCompare}
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
            {diff.value.failure !== null && <div className="hx-fed__diffnote">{diff.value.failure}</div>}
            {diff.value.failure === null && diff.value.text === null && (
              <span className="hx-sk hx-fed__wait" />
            )}
            {diff.value.text !== null && (
              <Suspense fallback={<span className="hx-sk hx-fed__wait" />}>
                {diff.value.text === after ? (
                  <div className="hx-fed__diffnote">{t('next.editor.diffSame')}</div>
                ) : (
                  <DiffView path={path} before={diff.value.text} after={after} layout={layout} />
                )}
              </Suspense>
            )}
          </div>
        )}
      </div>
      <div className="hx-fed__foot">
        <span className="hx-fed__lang">{languageLabel(path)}</span>
        <span>{t('next.editor.cursor', { line: place.line, col: place.col })}</span>
        {place.selected > 0 && <span>{t('next.editor.selected', { n: place.selected })}</span>}
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-head-btn"
          aria-label={t('next.editor.find')}
          title={t('next.editor.findHint')}
          onClick={() => {
            const view = live.current?.view;
            if (view) openSearchPanel(view);
          }}
        >
          <Icon name="search" size="sm" />
        </button>
        <span className="hx-fed__state" data-state={stateKey} role="status">
          {t(`next.editor.${stateKey}`)}
        </span>
      </div>
    </div>
  );
};

export default FileEditor;
