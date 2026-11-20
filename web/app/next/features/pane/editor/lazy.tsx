// The editor arrives in its own chunk (CodeMirror is most of it); until it does, the file shows as plain numbered lines
// in the box it will fill, so nothing jumps.
import LazyView from '@/next/ui/lazy-view';
import { t } from '@/next/i18n';
import type { IFileEditorProps } from '@/next/features/pane/editor/file-editor';
import type { NotebookRuntime } from '@/next/features/pane/notebook/notebook-api';
import { isNotebook } from '@/next/features/pane/notebook/notebook-model';
import '@/next/styles/editor.css';

const loadEditor = () => import('@/next/features/pane/editor/file-editor');
const loadNotebook = () => import('@/next/features/pane/notebook/notebook-view');

const LINES_SHOWN = 200;

const Plain = ({ text }: { text: string }) => (
  <div className="hx-fed" data-kind="plain">
    <div className="hx-fed__main">
      <div className="hx-code hx-pane__code">
        <div className="hx-code__body">
          {text
            .split('\n')
            .slice(0, LINES_SHOWN)
            .map((line, i) => (
              // The lines of a file have no ids; their place is what they are.
              <div key={i} className="hx-code__line">
                <span className="hx-code__ln">{i + 1}</span>
                <span>{line}</span>
              </div>
            ))}
        </div>
      </div>
    </div>
  </div>
);

const LazyFileEditor = (props: IFileEditorProps & { runtime?: NotebookRuntime }) => (
  <LazyView
    load={isNotebook(props.path) && !props.compare && !props.at ? loadNotebook : loadEditor}
    props={props}
    title={t('next.load.editor')}
    fallback={<Plain text={props.initial.text} />}
    below={<Plain text={props.initial.text} />}
  />
);

export default LazyFileEditor;
