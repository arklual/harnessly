// Real notebook cells under the complete app stylesheet, including other features' selectors. No production API,
// kernel, file write or upload: a selected file is read only in this browser for visual regression checks.
import { useEffect, useState } from 'react';
import NotebookView from '@/next/features/pane/notebook/notebook-view';
import PaneHead from '@/next/features/pane/pane-head';
import { pinLocale } from '@/next/i18n';
import type { FileContent } from '@/next/protocol/v2';
import '@/next/styles/order';
import '@/next/styles/editor.css';
import '@/next/dev/notebook-gallery.css';

const params = new URLSearchParams(window.location.search);
pinLocale(params.get('lang') === 'ru' ? 'ru' : 'en');

const SAMPLE = JSON.stringify({
  nbformat: 4,
  nbformat_minor: 5,
  metadata: { kernelspec: { name: 'project-python', display_name: 'Project Python', language: 'python' } },
  cells: [
    {
      cell_type: 'markdown',
      metadata: {},
      source:
        '# Notebook rendering regression\n\nMarkdown, **code** and saved results must stay visible in a long notebook.',
    },
    ...Array.from({ length: 73 }, (_, i) => ({
      cell_type: 'code',
      metadata: {},
      source: `value = ${i + 1}\nprint(value)`,
      execution_count: i + 1,
      outputs: [{ output_type: 'stream', name: 'stdout', text: `${i + 1}\n` }],
    })),
  ],
});

const Gallery = () => {
  const [file, setFile] = useState({ name: 'regression.ipynb', text: SAMPLE, epoch: 0 });
  const [failure, setFailure] = useState<string | null>(null);
  const [state, setState] = useState({ dirty: false, saving: false });
  useEffect(() => {
    const root = document.documentElement;
    const dark = params.get('theme') === 'dark';
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
  }, []);
  return (
    <main className="hx-notebook-gallery">
      <div className="hx-notebook-gallery__tools">
        <label>
          Open notebook{' '}
          <input
            type="file"
            accept=".ipynb,application/json"
            onChange={(event) => {
              const selected = event.target.files?.[0];
              if (!selected) return;
              void selected.text().then(
                (text) => {
                  setFailure(null);
                  setFile((old) => ({ name: selected.name, text, epoch: old.epoch + 1 }));
                },
                () => setFailure('Could not read notebook'),
              );
            }}
          />
        </label>
        <span>{state.dirty ? 'Unsaved edits (memory only)' : 'No disk writes'}</span>
        {failure && <span role="alert">{failure}</span>}
      </div>
      <section className="hx-notebook-gallery__pane">
        <PaneHead lead={<strong>{file.name}</strong>}>
          <NotebookView
            key={file.epoch}
            path={file.name}
            initial={{ text: file.text, base: file.text, revision: 'gallery' }}
            onSave={async (text) =>
              ({
                path: file.name,
                content: text,
                revision: 'gallery-saved',
                bytes: text.length,
              }) as FileContent
            }
            onReload={() => undefined}
            onLeave={() => undefined}
            onHandle={() => undefined}
            onState={setState}
          />
        </PaneHead>
      </section>
    </main>
  );
};

export default Gallery;
