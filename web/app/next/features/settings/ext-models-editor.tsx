// The models of a provider in its card: the ones the provider lists come by themselves; the person adds the
// ones it does not list and gives any model a name and the levels of reasoning it takes. What they did is marked
// (manual, edited), and a refresh from the provider keeps it.
import { useEffect, useRef, useState } from 'react';

import { EXT_EFFORTS, type IExtModel, type TExtEffort } from '@/next/features/settings/ext-providers-api';
import { SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

/** A removed row's exit, as long as hx-extm-out in ext-models.css. */
const ROW_EXIT_MS = 160;
/** The machine's limit of a model's id and name (extproviders.MaxModelID). */
const MAX_ID = 200;

export interface IModelRow extends IExtModel {
  /** Stable across edits of the id. */
  key: string;
  /** The window as typed; it becomes `window` once it reads as one. */
  windowText: string;
}

export const modelRowsOf = (models: readonly IExtModel[]): IModelRow[] =>
  models.map((model, i) => ({
    ...model,
    key: `kept-${i}-${model.id}`,
    windowText: windowText(model.window),
  }));

/** The rows as the machine takes them: a row without an id is left out, a name that is the id is no name. */
export const modelsOf = (rows: readonly IModelRow[]): IExtModel[] =>
  rows.flatMap(({ key: _key, windowText: _text, ...model }): IExtModel[] => {
    const id = model.id.trim();
    if (id === '') return [];
    const label = model.label.trim();
    const efforts = EXT_EFFORTS.filter((e) => model.efforts?.includes(e));
    const defaultEffort =
      model.defaultEffort && efforts.includes(model.defaultEffort) ? model.defaultEffort : '';
    return [
      { ...model, id, label: label === '' ? id : label, efforts, defaultEffort, window: model.window ?? 0 },
    ];
  });

const shape = (models: readonly IExtModel[]) =>
  JSON.stringify(
    models.map((m) => [
      m.id,
      m.label,
      m.efforts ?? [],
      m.defaultEffort ?? '',
      m.window ?? 0,
      !!m.manual,
      !!m.edited,
    ]),
  );

/** The machine's limit of a window (extproviders.MaxWindow). */
const MAX_WINDOW = 100 * 1024 * 1024;

/** A window as the person types it: 131072, 128k, 1m (k and m are 1024 and 1024², as the vendors count them; a
 * round thousand typed in full stays as typed). null: not a window. '' is no window. */
export const parseWindow = (text: string): number | null => {
  const m = /^(\d+(?:[.,]\d+)?)\s*([kKmMкКмМ]?)$/.exec(text.trim());
  if (text.trim() === '') return 0;
  if (!m) return null;
  const unit = m[2]?.toLowerCase() ?? '';
  let mult = 1;
  if (unit === 'k' || unit === 'к') mult = 1024;
  if (unit === 'm' || unit === 'м') mult = 1024 * 1024;
  const n = Math.round(Number((m[1] ?? '').replace(',', '.')) * mult);
  return n > 0 && n <= MAX_WINDOW ? n : null;
};

/** A window as the field shows it: 128K, 1M, or the number when it is no round count of them. */
export const windowText = (n: number | undefined): string => {
  if (!n) return '';
  if (n % (1024 * 1024) === 0) return `${n / (1024 * 1024)}M`;
  if (n % 1024 === 0) return `${n / 1024}K`;
  return String(n);
};

/** Whether the rows say something else than the machine keeps. */
export const modelsDirty = (rows: readonly IModelRow[], kept: readonly IExtModel[]): boolean =>
  shape(modelsOf(rows)) !== shape(modelsOf(modelRowsOf(kept)));

const EFFORT_LABEL = {
  low: 'next.extProviders.effort.low',
  medium: 'next.extProviders.effort.medium',
  high: 'next.extProviders.effort.high',
  xhigh: 'next.extProviders.effort.xhigh',
  max: 'next.extProviders.effort.max',
  ultra: 'next.extProviders.effort.ultra',
} as const satisfies Record<TExtEffort, string>;

export const effortLabel = (e: TExtEffort) => t(EFFORT_LABEL[e]);

interface IExtModelsEditorProps {
  rows: readonly IModelRow[];
  /** A provider of the team's environment: its models are shown, not changed. */
  readOnly: boolean;
  disabled: boolean;
  onChange: (rows: IModelRow[]) => void;
}

const ExtModelsEditor = ({ rows, readOnly, disabled, onChange }: IExtModelsEditorProps) => {
  const [leaving, setLeaving] = useState<readonly string[]>([]);
  // The row just added: its id field takes the focus.
  const [added, setAdded] = useState<string | null>(null);
  const timers = useRef<number[]>([]);
  useEffect(() => () => timers.current.forEach((id) => window.clearTimeout(id)), []);

  const patch = (key: string, change: Partial<IModelRow>) =>
    onChange(
      rows.map((row) => {
        if (row.key !== key) return row;
        const next = { ...row, ...change };
        // A model the provider lists keeps the person's word from now on.
        return row.manual ? next : { ...next, edited: true };
      }),
    );
  const toggle = (row: IModelRow, effort: TExtEffort) => {
    const have = row.efforts ?? [];
    const efforts = have.includes(effort) ? have.filter((e) => e !== effort) : [...have, effort];
    const kept = row.defaultEffort && efforts.includes(row.defaultEffort) ? row.defaultEffort : '';
    patch(row.key, { efforts, defaultEffort: kept });
  };
  const remove = (key: string) => {
    setLeaving((list) => [...list, key]);
    const id = window.setTimeout(() => {
      setLeaving((list) => list.filter((k) => k !== key));
      onChange(rows.filter((row) => row.key !== key));
    }, ROW_EXIT_MS);
    timers.current.push(id);
  };
  const add = () => {
    const key = `own-${Date.now()}`;
    setAdded(key);
    onChange([
      ...rows,
      {
        key,
        id: '',
        label: '',
        efforts: [],
        defaultEffort: '',
        window: 0,
        windowText: '',
        manual: true,
        edited: false,
      },
    ]);
  };

  return (
    <div className="hx-extp__fld hx-extm">
      <span className="hx-extp__label">{t('next.extProviders.modelsTitle')}</span>
      <small className="hx-extp__hint">
        {rows.length === 0 ? t('next.extProviders.modelsEmptyHand') : t('next.extProviders.modelsHint')}
      </small>
      {rows.length > 0 && (
        <ul className="hx-extm__list">
          {rows.map((row) => (
            <li key={row.key} className="hx-extm__row" data-leaving={leaving.includes(row.key) || undefined}>
              <div className="hx-extm__names">
                <input
                  className="hx-set__input"
                  value={row.label === row.id && !row.manual ? '' : row.label}
                  maxLength={MAX_ID}
                  disabled={disabled || readOnly}
                  aria-label={t('next.extProviders.model.label')}
                  placeholder={row.id || t('next.extProviders.model.label')}
                  onChange={(event) => patch(row.key, { label: event.target.value })}
                />
                {row.manual && !readOnly ? (
                  <input
                    className="hx-set__input hx-set__input--mono"
                    value={row.id}
                    maxLength={MAX_ID}
                    disabled={disabled}
                    autoCapitalize="off"
                    spellCheck={false}
                    autoFocus={added === row.key}
                    aria-label={t('next.extProviders.model.id')}
                    placeholder={t('next.extProviders.model.idPlaceholder')}
                    onChange={(event) => patch(row.key, { id: event.target.value.replace(/\s/g, '') })}
                  />
                ) : (
                  <span className="hx-extm__id" title={row.id}>
                    {row.id}
                  </span>
                )}
              </div>
              <label className="hx-extm__window">
                <span className="hx-extm__window-label">{t('next.extProviders.model.window')}</span>
                <input
                  className="hx-set__input hx-set__input--mono"
                  value={row.windowText}
                  inputMode="numeric"
                  maxLength={12}
                  disabled={disabled || readOnly}
                  aria-invalid={parseWindow(row.windowText) === null || undefined}
                  placeholder={t('next.extProviders.model.windowPlaceholder')}
                  onChange={(event) => {
                    const text = event.target.value;
                    const window = parseWindow(text);
                    patch(row.key, window === null ? { windowText: text } : { windowText: text, window });
                  }}
                />
              </label>
              <div
                className="hx-extm__efforts"
                role="group"
                aria-label={t('next.extProviders.model.efforts')}
              >
                {EXT_EFFORTS.map((effort) => (
                  <button
                    key={effort}
                    type="button"
                    className="hx-extm__eff"
                    aria-pressed={row.efforts?.includes(effort) ?? false}
                    disabled={disabled || readOnly}
                    onClick={() => toggle(row, effort)}
                  >
                    {effortLabel(effort)}
                  </button>
                ))}
                {(row.efforts ?? []).length === 0 && (
                  <span className="hx-extm__none">{t('next.extProviders.model.noEfforts')}</span>
                )}
              </div>
              {(row.efforts ?? []).length > 1 && (
                <div
                  className="hx-extm__default"
                  role="radiogroup"
                  aria-label={t('next.extProviders.model.default')}
                >
                  <span className="hx-extm__window-label">{t('next.extProviders.model.default')}</span>
                  {[...(row.efforts ?? []), '' as const].map((effort: TExtEffort | '') => (
                    <button
                      key={effort || 'own'}
                      type="button"
                      role="radio"
                      className="hx-extm__eff"
                      aria-checked={(row.defaultEffort ?? '') === effort}
                      disabled={disabled || readOnly}
                      onClick={() => patch(row.key, { defaultEffort: effort })}
                    >
                      {effort === '' ? t('next.extProviders.model.defaultOwn') : effortLabel(effort)}
                    </button>
                  ))}
                </div>
              )}
              {row.manual && !readOnly && (
                <button
                  type="button"
                  className="hx-head-btn hx-extm__remove"
                  aria-label={t('next.extProviders.model.remove')}
                  disabled={disabled}
                  onClick={() => remove(row.key)}
                >
                  <Icon name="x" size="sm" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {!readOnly && (
        <SetButton tone="ghost" icon="plus" disabled={disabled} onClick={add}>
          {t('next.extProviders.model.add')}
        </SetButton>
      )}
    </div>
  );
};

export default ExtModelsEditor;
