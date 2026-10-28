// What a link without sign-in shows, as two quiet pickers beside «Что даёт ссылка» and the term: which version of the
// design («Эта версия» is a snapshot taken when the link is made, «Всегда последняя» follows the owner's edits) and
// which pages. Both are menus of the same kind as the term's.
import { useState } from 'react';
import { GUEST_VERSION, type TGuestVersion } from '@/next/features/designly/access/api';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { t } from '@/next/i18n';

/** A page of the design a link may be limited to: the board's path is its key. */
export interface IScopePage {
  path: string;
  title: string;
}

const VERSION_KEY: Record<TGuestVersion, string> = {
  latest: 'next.access.version.latest',
  pinned: 'next.access.version.pinned',
};
const VERSIONS = [GUEST_VERSION.pinned, GUEST_VERSION.latest] as const;

interface IVersionProps {
  value: TGuestVersion;
  onChange: (value: TGuestVersion) => void;
  disabled?: boolean;
}

export const VersionPicker = ({ value, onChange, disabled = false }: IVersionProps) => {
  const [open, setOpen] = useState(false);
  const label = t('next.access.version');
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={label}
      side="bottom"
      align="start"
      className="hx-acc-pop"
      trigger={
        <button type="button" className="hx-acc-pick" aria-label={label} disabled={disabled}>
          <span>{t(VERSION_KEY[value])}</span>
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <div role="menu" aria-label={label} className="hx-acc-menu">
        {VERSIONS.map((version) => (
          <button
            key={version}
            type="button"
            role="menuitemradio"
            aria-checked={value === version}
            className="hx-acc-mi"
            onClick={() => {
              setOpen(false);
              if (version !== value) onChange(version);
            }}
          >
            <span className="hx-acc-mi__text">{t(VERSION_KEY[version])}</span>
            <Icon name="check" size="sm" className="hx-acc-mi__check" />
          </button>
        ))}
      </div>
    </Popover>
  );
};

interface IPagesProps {
  pages: readonly IScopePage[];
  /** The chosen pages' paths; null is all of them. */
  value: readonly string[] | null;
  onChange: (value: string[] | null) => void;
  disabled?: boolean;
}

/** The button's words: «Все страницы», the page's own name when one is chosen, else how many. */
export const pagesLabel = (pages: readonly IScopePage[], value: readonly string[] | null): string => {
  if (value === null) return t('next.access.pages.all');
  if (value.length === 1) return pages.find((p) => p.path === value[0])?.title ?? t('next.access.pages.all');
  return t('next.access.pages.some', { n: value.length });
};

export const PagesPicker = ({ pages, value, onChange, disabled = false }: IPagesProps) => {
  const [open, setOpen] = useState(false);
  const label = t('next.access.pages');
  const chosen = value ?? [];

  const toggle = (path: string) => {
    const next = chosen.includes(path) ? chosen.filter((p) => p !== path) : [...chosen, path];
    // Every page chosen, or none left, is the same as all of them.
    onChange(next.length === 0 || next.length === pages.length ? null : next);
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={label}
      side="bottom"
      align="start"
      className="hx-acc-pop hx-acc-pop--pages"
      trigger={
        <button type="button" className="hx-acc-pick" aria-label={label} disabled={disabled}>
          <span>{pagesLabel(pages, value)}</span>
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <div role="menu" aria-label={label} className="hx-acc-menu">
        <button
          type="button"
          role="menuitemradio"
          aria-checked={value === null}
          className="hx-acc-mi"
          onClick={() => {
            if (value !== null) onChange(null);
          }}
        >
          <span className="hx-acc-mi__text">{t('next.access.pages.all')}</span>
          <Icon name="check" size="sm" className="hx-acc-mi__check" />
        </button>
        <span className="hx-acc-mi__sep" role="separator" />
        {pages.map((page) => (
          <button
            key={page.path}
            type="button"
            role="menuitemcheckbox"
            aria-checked={chosen.includes(page.path)}
            className="hx-acc-mi"
            onClick={() => toggle(page.path)}
          >
            <span className="hx-acc-mi__box" aria-hidden="true">
              <Icon name="check" size="sm" />
            </span>
            <span className="hx-acc-mi__text">{page.title}</span>
          </button>
        ))}
      </div>
    </Popover>
  );
};
