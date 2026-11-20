// Cells share the file editor's CodeMirror setup rather than a second editor/theme implementation.
import { useEffect, useEffectEvent, useRef } from 'react';
import { Compartment, EditorState, Prec } from '@codemirror/state';
import { EditorView, keymap } from '@codemirror/view';
import { extensions, language } from '@/next/features/pane/editor/setup';
import { loadLanguage } from '@/next/features/pane/editor/language';

interface SourceProps {
  text: string;
  label: string;
  kind: 'code' | 'markdown' | 'raw';
  readOnly: boolean;
  onChange: (text: string) => void;
  onRun: () => void;
}

const NotebookSource = ({ text, label, kind, readOnly, onChange, onRun }: SourceProps) => {
  const host = useRef<HTMLDivElement>(null);
  const live = useRef<{ view: EditorView; editable: Compartment } | null>(null);
  const changed = useEffectEvent((value: string) => onChange(value));
  const run = useEffectEvent(() => {
    if (!readOnly && kind === 'code') onRun();
  });
  // The initial document is installed once; later changes are synchronized below without resetting the undo stack.
  const start = useRef({ text, label });
  useEffect(() => {
    if (!host.current) return undefined;
    const editable = new Compartment();
    const view = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: start.current.text,
        extensions: [
          keymap.of([
            {
              key: 'Shift-Enter',
              run: () => {
                run();
                return true;
              },
            },
          ]),
          extensions({
            eol: '\n',
            text: start.current.text,
            markdown: kind === 'markdown',
            wrap: kind !== 'code',
            onUpdate: (update) => {
              if (update.docChanged) changed(update.state.doc.toString());
            },
          }),
          editable.of(EditorState.readOnly.of(false)),
          Prec.high(EditorView.contentAttributes.of({ 'aria-label': start.current.label })),
          Prec.high(
            EditorView.theme({
              '&': { height: 'auto' },
              '.cm-content': { padding: '8px 0' },
              '.cm-scroller': { maxHeight: '440px' },
            }),
          ),
        ],
      }),
    });
    live.current = { view, editable };
    let alive = true;
    void loadLanguage(kind === 'code' ? 'cell.py' : kind === 'markdown' ? 'cell.md' : 'cell.txt').then(
      (support) => {
        if (alive && support) view.dispatch({ effects: language.reconfigure(support) });
      },
    );
    return () => {
      alive = false;
      live.current = null;
      view.destroy();
    };
  }, [kind]);
  useEffect(() => {
    const entry = live.current;
    if (entry)
      entry.view.dispatch({
        effects: entry.editable.reconfigure([
          EditorState.readOnly.of(readOnly),
          EditorView.editable.of(!readOnly),
        ]),
      });
  }, [readOnly]);
  useEffect(() => {
    const view = live.current?.view;
    if (view && view.state.doc.toString() !== text)
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
  }, [text]);
  return <div className="hx-jnb__source" ref={host} />;
};

export default NotebookSource;
