// «Отложенные» of «+»: the drafts put aside in this field, newest first. A tap brings one back after the text in the
// field, × forgets it. It sits in the «+» panel in place of the menu, as the pickers do (one implementation for a
// popover and a sheet).
import Icon from '@/next/ui/icon';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';

export interface IStashRow {
  id: string;
  /** The first words of the draft; empty for a draft of files only. */
  preview: string;
  files: number;
  at: number;
}

export interface IStash {
  rows: readonly IStashRow[];
  /** The field has something to put aside. */
  canStash: boolean;
  onStash: () => void;
  onRestore: (id: string) => void;
  onDrop: (id: string) => void;
}

interface IStashListProps {
  rows: readonly IStashRow[];
  now: number;
  onRestore: (id: string) => void;
  onDrop: (id: string) => void;
  onBack: () => void;
}

const StashList = ({ rows, now, onRestore, onDrop, onBack }: IStashListProps) => (
  <div className="hx-plus__pane hx-pick" data-sub="true" data-kind="stash">
    <div className="hx-pick__head">
      <button type="button" className="hx-pick__back" aria-label={t('next.attach.back')} onClick={onBack}>
        <Icon name="chevron-left" size="sm" />
      </button>
      <span className="hx-pick__title">{t('next.stash.title')}</span>
    </div>
    <div className="hx-pick__list" role="list" aria-label={t('next.stash.title')}>
      {rows.length === 0 && <div className="hx-pick__state">{t('next.stash.empty')}</div>}
      {rows.map((row) => (
        <div key={row.id} className="hx-stash__row" role="listitem">
          <button type="button" className="hx-pick__row" onClick={() => onRestore(row.id)}>
            <span className="hx-pick__text">
              <span className="hx-pick__name">{row.preview || t('next.stash.files', { n: row.files })}</span>
              <span className="hx-pick__hint">
                {[
                  formatAgo(row.at, now),
                  row.preview && row.files > 0 ? t('next.stash.files', { n: row.files }) : '',
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            </span>
          </button>
          <button
            type="button"
            className="hx-stash__x"
            aria-label={t('next.stash.drop')}
            onClick={() => onDrop(row.id)}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
      ))}
    </div>
  </div>
);

export default StashList;
