// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import ExtModelsEditor, {
  modelRowsOf,
  parseWindow,
  modelsDirty,
  modelsOf,
  type IModelRow,
} from '@/next/features/settings/ext-models-editor';
import type { IExtModel } from '@/next/features/settings/ext-providers-api';
import { smartChoices } from '@/next/features/settings/smart-section';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const kept: IExtModel[] = [
  { id: 'deepseek-chat', label: 'deepseek-chat', efforts: [], defaultEffort: '', window: 131072 },
];

const Harness = ({ onRows }: { onRows: (rows: readonly IModelRow[]) => void }) => {
  const [rows, setRows] = useState(modelRowsOf(kept));
  return (
    <ExtModelsEditor
      rows={rows}
      readOnly={false}
      disabled={false}
      onChange={(next) => {
        setRows(next);
        onRows(next);
      }}
    />
  );
};

describe('the models of a provider', () => {
  it('adds a model by hand with its name and levels, and marks a listed one the person named', () => {
    let rows: readonly IModelRow[] = [];
    render(<Harness onRows={(next) => (rows = next)} />);
    fireEvent.change(screen.getAllByLabelText('Name')[0], { target: { value: 'DeepSeek V4' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add a model' }));
    fireEvent.change(screen.getByLabelText('Model id'), { target: { value: 'deepseek reasoner' } });
    fireEvent.click(screen.getAllByRole('button', { name: 'High' })[1]);
    fireEvent.click(screen.getAllByRole('button', { name: 'Low' })[1]);
    // Two levels: one of them is the default.
    fireEvent.click(screen.getByRole('radio', { name: 'High' }));
    expect(screen.getAllByLabelText('Context window')[0]).toHaveProperty('value', '128K');
    const models = modelsOf(rows);
    expect(models).toEqual([
      {
        id: 'deepseek-chat',
        label: 'DeepSeek V4',
        efforts: [],
        defaultEffort: '',
        window: 131072,
        edited: true,
      },
      {
        id: 'deepseekreasoner',
        label: 'deepseekreasoner',
        efforts: ['low', 'high'],
        defaultEffort: 'high',
        window: 0,
        manual: true,
        edited: false,
      },
    ]);
    expect(modelsDirty(rows, kept)).toBe(true);
    expect(modelsDirty(modelRowsOf(kept), kept)).toBe(false);
  });

  it('reads a window as the person types it', () => {
    expect(parseWindow('128k')).toBe(131072);
    expect(parseWindow('1M')).toBe(1048576);
    expect(parseWindow('64000')).toBe(64000);
    expect(parseWindow('')).toBe(0);
    expect(parseWindow('много')).toBeNull();
  });

  it('offers a model of a smart feature at each of its levels', () => {
    const { choices } = smartChoices(
      {
        id: 'f',
        title: 'F',
        description: '',
        live: true,
        agents: false,
        offable: false,
        choice: null,
        failure: null,
      },
      [{ id: 'ds', name: 'DeepSeek', models: [{ id: 'r1', label: 'R1', efforts: ['low', 'high'] }] }],
      { claude: [], codex: [] },
    );
    expect(choices.map((c) => c.value)).toEqual([null, 'ds/r1', 'ds/r1#low', 'ds/r1#high']);
    expect(choices[3]?.label).toBe('DeepSeek · R1 · High');
  });

  it("offers the machine's agents' models on their plan to any feature, and «off» only where it may be off (HRN-1492)", () => {
    const agentModels = {
      claude: [{ id: 'haiku', label: 'Haiku' }],
      codex: [{ id: 'gpt-5-mini', label: 'GPT-5 mini' }],
    };
    const feature = {
      id: 'search-answer',
      title: 'S',
      description: '',
      live: true,
      agents: true,
      offable: false,
      choice: null,
      failure: null,
    };
    const { choices } = smartChoices(feature, [], agentModels);
    expect(choices.map((c) => c.value)).toEqual([null, 'claude/haiku', 'codex/gpt-5-mini']);
    expect(choices[1]?.sub).toBeTruthy();
    const summary = smartChoices(
      { ...feature, id: 'notify-summary', offable: true },
      [],
      agentModels,
    ).choices;
    expect(summary.map((c) => c.value)).toContain('off/');
  });
});
