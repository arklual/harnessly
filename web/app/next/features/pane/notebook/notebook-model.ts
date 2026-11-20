// Notebook JSON stays lossless: unknown metadata, attachments and cell fields survive an edit or a run.
import { cleanHtml } from '@/next/features/chat/html-doc';
export interface NotebookOutput {
  output_type: string;
  name?: string;
  text?: string | string[];
  data?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  execution_count?: number | null;
  ename?: string;
  evalue?: string;
  traceback?: string[];
  [key: string]: unknown;
}

export interface NotebookCell {
  cell_type: 'code' | 'markdown' | 'raw';
  source: string | string[];
  metadata: Record<string, unknown>;
  outputs?: NotebookOutput[];
  execution_count?: number | null;
  [key: string]: unknown;
}

export interface Notebook {
  nbformat: number;
  nbformat_minor: number;
  metadata: Record<string, unknown>;
  cells: NotebookCell[];
  [key: string]: unknown;
}

export const notebookText = (value: unknown): string =>
  typeof value === 'string'
    ? value
    : Array.isArray(value) && value.every((part) => typeof part === 'string')
      ? value.join('')
      : '';

const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

export const parseNotebook = (text: string): Notebook => {
  const value: unknown = JSON.parse(text);
  if (!record(value) || value.nbformat !== 4 || !Array.isArray(value.cells) || !record(value.metadata))
    throw new Error('Expected a Jupyter notebook in nbformat 4.');
  for (const cell of value.cells) {
    if (
      !record(cell) ||
      !['code', 'markdown', 'raw'].includes(String(cell.cell_type)) ||
      !(
        typeof cell.source === 'string' ||
        (Array.isArray(cell.source) && cell.source.every((s) => typeof s === 'string'))
      ) ||
      !record(cell.metadata) ||
      (cell.cell_type === 'code' &&
        (!Array.isArray(cell.outputs) ||
          !cell.outputs.every((o) => record(o) && typeof o.output_type === 'string')))
    )
      throw new Error('Invalid notebook cell.');
  }
  return value as unknown as Notebook;
};

// Kernelspec names identify environments (e.g. a project's venv), not languages or launch commands.
// Language metadata recognizes custom Python names without ever trusting kernelspec argv/executable fields.
export const isPythonNotebook = (notebook: Notebook): boolean => {
  const spec = record(notebook.metadata.kernelspec) ? notebook.metadata.kernelspec : {};
  const info = record(notebook.metadata.language_info) ? notebook.metadata.language_info : {};
  const language = notebookText(spec.language || info.name)
    .trim()
    .toLowerCase();
  if (language) return language === 'python' || language === 'python3' || language === 'ipython';
  const name = notebookText(spec.name).trim().toLowerCase();
  return name === '' || name === 'python' || name === 'python3';
};

export const writeNotebook = (notebook: Notebook): string => `${JSON.stringify(notebook, null, 1)}\n`;
export const isNotebook = (path: string): boolean => /\.ipynb$/i.test(path);

// Saved HTML uses the existing static-preview cleaner, a restrictive CSP, and a scriptless iframe.
export const notebookHtml = (html: string): string =>
  `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; form-action 'none'; base-uri 'none'"><style>body{font:14px system-ui;margin:8px;overflow:auto}table{border-collapse:collapse}td,th{padding:4px 8px;border:1px solid #aaa}pre{white-space:pre-wrap}</style></head><body>${cleanHtml(html)}</body></html>`;
