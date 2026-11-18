// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { isNotebook, notebookHtml, notebookText, parseNotebook, writeNotebook } from './notebook-model';

const fixture = () => ({
  nbformat: 4,
  nbformat_minor: 5,
  metadata: { kernelspec: { name: 'python3' }, custom: [1, 2] },
  cells: [
    {
      id: 'text',
      cell_type: 'markdown',
      source: ['# Example\n', 'Hello'],
      metadata: {},
      attachments: { picture: {} },
    },
    {
      id: 'code',
      cell_type: 'code',
      source: '1 + 1',
      metadata: { tags: ['example'] },
      execution_count: 1,
      outputs: [
        { output_type: 'execute_result', execution_count: 1, data: { 'text/plain': ['2'] }, metadata: {} },
      ],
    },
  ],
  extra: 'keep',
});

describe('notebook files', () => {
  it('preserves unknown fields, metadata, attachments and saved output', () => {
    const notebook = parseNotebook(JSON.stringify(fixture()));
    notebook.cells[1].source = '2 + 2';
    const saved = JSON.parse(writeNotebook(notebook));
    expect(saved).toEqual({
      ...fixture(),
      cells: [fixture().cells[0], { ...fixture().cells[1], source: '2 + 2' }],
    });
  });
  it('reads both forms of notebook multiline strings', () => {
    expect(notebookText(['a\n', 'b'])).toBe('a\nb');
    expect(notebookText('a')).toBe('a');
    expect(notebookText(['a', 1])).toBe('');
    expect(notebookText({})).toBe('');
  });
  it('rejects unsupported formats and malformed cells before rendering', () => {
    for (const value of [
      null,
      [],
      {},
      { ...fixture(), nbformat: 3 },
      { ...fixture(), cells: [null] },
      { ...fixture(), cells: [{ cell_type: 'code', metadata: {}, source: {}, outputs: [] }] },
      { ...fixture(), cells: [{ cell_type: 'code', metadata: {}, source: '', outputs: [null] }] },
    ])
      expect(() => parseNotebook(JSON.stringify(value))).toThrow();
  });
  it('recognizes only notebook extensions', () => {
    expect(isNotebook('/work/EXAMPLE.IPYNB')).toBe(true);
    expect(isNotebook('/work/example.ipynb.py')).toBe(false);
  });
  it('puts a restrictive CSP before any saved HTML', () => {
    const html = notebookHtml(
      '<meta http-equiv="refresh" content="0;url=https://example.com"><script>fetch("/api")</script><table><tr><td>result</td></tr></table>',
    );
    expect(html).not.toContain('<script>');
    expect(html).not.toContain('http-equiv="refresh"');
    expect(html).toContain('<td>result</td>');
    expect(html).toContain("default-src 'none'");
    expect(html).not.toContain('allow-scripts');
  });
});
