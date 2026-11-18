// The file editor's save/draft contract also holds for notebooks. Executing never writes the file itself.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import { Button, RoundButton } from '@/next/ui/button';
import { WordedError } from '@/next/core/net/worded';
import FileEditor, { type IFileEditorProps } from '@/next/features/pane/editor/file-editor';
import MdPreview from '@/next/features/pane/editor/md-preview';
import { HeadTools } from '@/next/features/pane/pane-head';
import NotebookOutputView from './notebook-output';
import NotebookSource from './notebook-source';
import {
  notebookRequest,
  type ExecutionResult,
  type KernelState,
  type NotebookRuntime,
} from './notebook-api';
import { notebookText, parseNotebook, writeNotebook, type Notebook } from './notebook-model';
import '@/next/styles/notebook.css';

export interface NotebookViewProps extends IFileEditorProps {
  runtime?: NotebookRuntime;
}

const read = (text: string): Notebook | null => {
  try {
    return parseNotebook(text);
  } catch {
    return null;
  }
};

// Invalid/old notebooks remain editable as JSON rather than trapping the person in a broken viewer.
const NotebookView = (props: NotebookViewProps) => {
  const [parsed] = useState(() => read(props.initial.text));
  return parsed === null ? <FileEditor {...props} /> : <NotebookEditor {...props} notebook={parsed} />;
};

const NotebookEditor = ({
  notebook,
  runtime: executionRuntime,
  readOnly = false,
  ...props
}: NotebookViewProps & { notebook: Notebook }) => {
  const runtime = readOnly ? undefined : executionRuntime;
  const { path, initial, onSave, onReload, onLeave, onState, onHandle } = props;
  const live = useRef({ notebook, text: initial.text, base: initial.base, revision: initial.revision });
  const [book, setBook] = useState(notebook);
  const [dirty, setDirty] = useState(initial.text !== initial.base);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<{ text: string; conflict: boolean } | null>(null);
  const [kernel, setKernel] = useState<KernelState['state']>('stopped');
  const [running, setRunning] = useState<number | null>(null);
  const [allowed, setAllowed] = useState(false);
  const [editing, setEditing] = useState<number | null>(null);
  const executing = useRef(false);
  const savingNow = useRef(false);
  const mounted = useRef(true);
  const stopped = useRef(false);

  const change = (next: Notebook) => {
    if (readOnly) return;
    live.current.notebook = next;
    live.current.text = writeNotebook(next);
    setBook(next);
    setDirty(live.current.text !== live.current.base);
  };
  const source = (index: number, text: string) =>
    change({
      ...live.current.notebook,
      cells: live.current.notebook.cells.map((cell, i) => (i === index ? { ...cell, source: text } : cell)),
    });
  const report = useEffectEvent(() => onState({ dirty, saving }));
  useEffect(() => report(), [dirty, saving]);
  const leave = useEffectEvent(() => {
    const l = live.current;
    // A file editor is unmounted on navigation. Do not leave an unseen run-all queue executing code.
    if (executing.current && runtime) void notebookRequest(runtime, path, 'interrupt').catch(() => undefined);
    onLeave(l.text !== l.base ? { text: l.text, base: l.base, revision: l.revision } : null);
    onHandle(null);
  });
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      stopped.current = true;
      leave();
    };
  }, []);

  const save = async (over = false): Promise<boolean> => {
    const l = live.current;
    if (readOnly || savingNow.current || executing.current) return false;
    if (!over && l.text === l.base) return true;
    const text = l.text;
    savingNow.current = true;
    setSaving(true);
    setNotice(null);
    try {
      const file = await onSave(text, l.revision, over);
      l.base = text;
      l.revision = file.revision;
      if (mounted.current) setDirty(l.text !== l.base);
      return true;
    } catch (error) {
      if (mounted.current)
        setNotice({
          text: error instanceof Error ? error.message : t('next.failure.http'),
          conflict: error instanceof WordedError && error.code === 'conflict',
        });
      return false;
    } finally {
      savingNow.current = false;
      if (mounted.current) setSaving(false);
    }
  };
  const handOver = useEffectEvent(() => onHandle({ save: () => save() }));
  useEffect(() => {
    handOver();
  }, []);
  const press = useEffectEvent((event: KeyboardEvent) => {
    const target = event.target as Element | null;
    if (
      !(event.ctrlKey || event.metaKey) ||
      event.altKey ||
      event.key.toLowerCase() !== 's' ||
      !target?.closest('.hx-nb')
    )
      return;
    event.preventDefault();
    void save();
  });
  useEffect(() => {
    document.addEventListener('keydown', press);
    return () => document.removeEventListener('keydown', press);
  }, []);

  const machine = runtime?.machine;
  const conv = runtime?.conv;
  useEffect(() => {
    if (!machine || !conv) return undefined;
    let alive = true;
    void notebookRequest<KernelState>({ machine, conv }, path).then(
      (state) => {
        if (alive) setKernel(state.state);
      },
      () => {
        /* Viewing never requires an installed execution service. */
      },
    );
    return () => {
      alive = false;
    };
  }, [machine, conv, path]);

  const fail = (error: unknown) => {
    if (mounted.current)
      setNotice({ text: error instanceof Error ? error.message : t('next.failure.http'), conflict: false });
  };
  const refresh = async () => {
    if (!runtime || !mounted.current) return;
    try {
      const state = await notebookRequest<KernelState>(runtime, path);
      if (mounted.current) setKernel(state.state);
    } catch {
      if (mounted.current) setKernel('stopped');
    }
  };
  const checkKernel = useEffectEvent(() => refresh());
  useEffect(() => {
    if (kernel !== 'busy' || running !== null) return undefined;
    const timer = window.setInterval(() => {
      void checkKernel();
    }, 2000);
    return () => window.clearInterval(timer);
  }, [kernel, running]);
  const execute = async (indices: number[]) => {
    if (!runtime || !allowed || executing.current || savingNow.current) return;
    executing.current = true;
    stopped.current = false;
    setKernel('busy');
    setNotice(null);
    try {
      for (const index of indices) {
        if (stopped.current || !mounted.current) break;
        const cell = live.current.notebook.cells[index];
        if (cell.cell_type !== 'code') continue;
        setRunning(index);
        const result = await notebookRequest<ExecutionResult>(
          runtime,
          path,
          'execute',
          notebookText(cell.source),
        );
        if (!mounted.current) break;
        change({
          ...live.current.notebook,
          cells: live.current.notebook.cells.map((old, i) =>
            i === index
              ? {
                  ...old,
                  outputs: result.outputs,
                  execution_count: result.execution_count,
                }
              : old,
          ),
        });
        if (result.status !== 'ok') break;
      }
    } catch (error) {
      fail(error);
    } finally {
      executing.current = false;
      if (mounted.current) {
        setRunning(null);
        await refresh();
      }
    }
  };
  const control = async (action: 'interrupt' | 'restart' | 'shutdown') => {
    if (!runtime) return;
    stopped.current = true;
    try {
      const result = await notebookRequest<KernelState>(runtime, path, action);
      if (mounted.current) setKernel(result.state);
    } catch (error) {
      fail(error);
    }
  };
  const busy = running !== null || kernel === 'busy';
  const codeCells = book.cells.flatMap((cell, i) => (cell.cell_type === 'code' ? [i] : []));
  const kernelName = book.metadata.kernelspec;
  const declaredKernel =
    typeof kernelName === 'object' && kernelName !== null && 'name' in kernelName
      ? kernelName.name
      : undefined;
  const compatible = declaredKernel === undefined || declaredKernel === 'python3';

  return (
    <div className="hx-fed hx-nb" data-kind="notebook">
      <HeadTools bar="hx-fed__bar">
        <span className="hx-nb__kernel" role="status">
          {t(`next.notebook.state.${kernel}`)}
        </span>
        <RoundButton
          size="sm"
          variant="ghost"
          icon="play"
          label={t('next.notebook.runAll')}
          disabled={!runtime || !allowed || !compatible || busy || saving || codeCells.length === 0}
          onClick={() => void execute(codeCells)}
        />
        <RoundButton
          size="sm"
          variant="ghost"
          icon="square"
          label={t('next.notebook.interrupt')}
          disabled={!runtime || !busy}
          onClick={() => void control('interrupt')}
        />
        <RoundButton
          size="sm"
          variant="ghost"
          icon="rotate-cw"
          label={t('next.notebook.restart')}
          disabled={!runtime || !allowed || busy || kernel === 'stopped'}
          onClick={() => void control('restart')}
        />
        <RoundButton
          size="sm"
          variant="ghost"
          icon="circle-stop"
          label={t('next.notebook.shutdown')}
          disabled={!runtime || kernel === 'stopped'}
          onClick={() => void control('shutdown')}
        />
      </HeadTools>
      {notice && (
        <div className="hx-fed__notice" role="alert">
          <span className="hx-fed__notice-text">{notice.text}</span>
          {notice.conflict && (
            <>
              <Button size="sm" variant="ghost" onClick={onReload}>
                {t('next.editor.reload')}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => void save(true)}>
                {t('next.editor.overwrite')}
              </Button>
            </>
          )}
          <Button size="sm" variant="ghost" onClick={() => setNotice(null)}>
            {t('next.editor.dismiss')}
          </Button>
        </div>
      )}
      <div className="hx-nb__body">
        {!compatible && (
          <p className="hx-nb__warning" role="status">
            {t('next.notebook.pythonOnly')}
          </p>
        )}
        {!allowed && runtime && compatible && (
          <div className="hx-nb__trust">
            <p>{t('next.notebook.trustNotice')}</p>
            <Button size="sm" variant="ghost" onClick={() => setAllowed(true)}>
              {t('next.notebook.allow')}
            </Button>
          </div>
        )}
        {book.cells.length === 0 && <p className="hx-nb__muted">{t('next.notebook.empty')}</p>}
        {book.cells.map((cell, index) => (
          <section
            key={index}
            className="hx-nb__cell"
            data-running={running === index || undefined}
            aria-label={t('next.notebook.cell', { n: index + 1 })}
          >
            <div className="hx-nb__cell-head">
              <span>
                {cell.cell_type === 'code'
                  ? `[${running === index ? '*' : (cell.execution_count ?? ' ')}]`
                  : cell.cell_type === 'markdown'
                    ? 'Markdown'
                    : t('next.notebook.raw')}
              </span>
              {cell.cell_type === 'code' && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!runtime || !allowed || !compatible || busy || saving}
                  onClick={() => void execute([index])}
                >
                  {t('next.notebook.run')}
                </Button>
              )}
              {cell.cell_type === 'markdown' && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setEditing(editing === index ? null : index)}
                >
                  {t(editing === index ? 'next.editor.preview' : 'next.editor.edit')}
                </Button>
              )}
            </div>
            {cell.cell_type === 'markdown' && editing !== index ? (
              <MdPreview md={notebookText(cell.source)} />
            ) : (
              <NotebookSource
                label={t('next.notebook.source', { n: index + 1 })}
                text={notebookText(cell.source)}
                kind={cell.cell_type}
                readOnly={readOnly || busy}
                onChange={(text) => source(index, text)}
                onRun={() => {
                  if (!busy) void execute([index]);
                }}
              />
            )}
            {cell.cell_type === 'code' && cell.outputs && cell.outputs.length > 0 && (
              <div className="hx-nb__outputs">
                {cell.outputs.slice(0, 200).map((output, i) => (
                  <NotebookOutputView key={i} output={output} />
                ))}
                {cell.outputs.length > 200 && <p>{t('next.notebook.outputTruncated')}</p>}
              </div>
            )}
          </section>
        ))}
      </div>
      <div className="hx-fed__status">
        <span>{t('next.notebook.label')}</span>
        <span>{t(saving ? 'next.editor.saving' : dirty ? 'next.editor.dirty' : 'next.editor.saved')}</span>
      </div>
    </div>
  );
};

export default NotebookView;
