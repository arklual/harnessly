import { t } from '@/next/i18n';
import MdPreview from '@/next/features/pane/editor/md-preview';
import { notebookHtml, notebookText, type NotebookOutput } from './notebook-model';

// Terminal escapes are not markup; discard colour/control sequences rather than interpreting saved output.
const plain = (text: string) => text.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, '');

const NotebookOutputView = ({ output }: { output: NotebookOutput }) => {
  if (output.output_type === 'stream')
    return <pre data-stderr={output.name === 'stderr' || undefined}>{plain(notebookText(output.text))}</pre>;
  if (output.output_type === 'error') {
    const trace = Array.isArray(output.traceback)
      ? notebookText(output.traceback.map((line) => `${line}\n`))
      : '';
    return (
      <pre data-stderr role="status">
        {plain(trace || `${notebookText(output.ename)}: ${notebookText(output.evalue)}`)}
      </pre>
    );
  }
  const data = output.data;
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null;
  for (const mime of ['image/png', 'image/jpeg', 'image/gif']) {
    const image = notebookText(data[mime]);
    if (image)
      return (
        <img alt={t('next.notebook.outputImage')} src={`data:${mime};base64,${image.replace(/\s/g, '')}`} />
      );
  }
  const svg = notebookText(data['image/svg+xml']);
  if (svg)
    return (
      <img
        alt={t('next.notebook.outputImage')}
        src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
      />
    );
  const html = notebookText(data['text/html']);
  if (html)
    return (
      <iframe
        title={t('next.notebook.outputHtml')}
        sandbox=""
        referrerPolicy="no-referrer"
        srcDoc={notebookHtml(html)}
      />
    );
  const markdown = notebookText(data['text/markdown']);
  if (markdown) return <MdPreview md={markdown} />;
  const text = notebookText(data['text/plain']);
  if (text) return <pre>{plain(text)}</pre>;
  if (data['application/json'] !== undefined)
    return <pre>{JSON.stringify(data['application/json'], null, 2)}</pre>;
  return <p className="hx-nb__muted">{t('next.notebook.unsupportedOutput')}</p>;
};

export default NotebookOutputView;
