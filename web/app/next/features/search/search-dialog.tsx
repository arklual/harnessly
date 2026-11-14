import { Dialog } from '@base-ui/react/dialog';
import { Menu } from '@base-ui/react/menu';
import { isMac } from '@/next/features/keys/chord';
import { useKeyHint } from '@/next/features/keys/keys-help';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import Switch from '@/next/ui/switch';
import { t } from '@/next/i18n';
import { usePortalContainer } from '@/next/ui/portal';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton, useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import {
  useRowDetail,
  type IDetailItem,
  type IDetailSource,
  type ISearchDetail,
  type TDetailAct,
} from '@/next/features/search/detail';
import { partsOf } from '@/next/features/search/highlight';
import SearchSide, {
  SIDE_ACT,
  SIDE_MENU_CLOSED,
  type ISideAction,
  type ISideMenu,
  type TSideAct,
} from '@/next/features/search/search-side';
import { SETTING_OFF, SETTING_ON } from '@/next/features/search/sources-api';
import { SUBJECT, type TSearchSubject } from '@/next/features/search/subject';
import { layoutKey, layoutOf, steadyLists, type TShownLayout } from '@/next/core/steady-order';
import type { SettingsSection } from '@/next/routes/routes';

export interface ISearchScope {
  id: string;
  label: string;
  /** What the tab is, said when the pointer rests on it (a connected system's own name and address). */
  hint?: string;
  /** How many rows the words found in it; absent before anything is typed. */
  count?: number;
}

/** Where a row leads when it is opened. A row without one is a chat (its id) or a message (its target). */
export const ROW_GO = {
  setting: 'setting',
  section: 'section',
  design: 'design',
  link: 'link',
  review: 'review',
  newChat: 'newChat',
  designs: 'designs',
  chat: 'chat',
} as const;

export type TSearchGo =
  /** A setting of the catalog, in the section of Settings it is changed in. */
  | { k: typeof ROW_GO.setting; key: string; section: SettingsSection }
  /** A section of Settings; null is Settings itself. */
  | { k: typeof ROW_GO.section; section: SettingsSection | null }
  /** `board` opens the design at that board. */
  | { k: typeof ROW_GO.design; id: string; board?: string }
  | { k: typeof ROW_GO.chat; id: string }
  /** An address outside the page: a document, an issue, a wiki page. */
  | { k: typeof ROW_GO.link; url: string }
  /** A merge request, opened in the review. */
  | { k: typeof ROW_GO.review; url: string }
  | { k: typeof ROW_GO.newChat }
  | { k: typeof ROW_GO.designs };

export const ROW_WIDGET = { toggle: 'toggle', pick: 'pick' } as const;

/** The control a setting's row carries: the setting is changed without leaving the search. */
export type ISearchWidget =
  | { kind: typeof ROW_WIDGET.toggle; on: boolean }
  | {
      kind: typeof ROW_WIDGET.pick;
      value: string;
      label: string;
      options: readonly { value: string; label: string }[];
    };

export interface ISearchRow {
  id: string;
  /** chat, message, file, link, design: the icon's look. */
  kind: string;
  icon: string;
  title: string;
  /** The words around the match, for a message. */
  snippet?: string;
  /** Where it is: a chat's project, a file's chat, a date. */
  meta: string;
  /** For a message: the chat it is in and the place in it that opens. */
  target?: { chatId: string; entryId: string; turnId: string | null };
  go?: TSearchGo;
  widget?: ISearchWidget;
  /** What the side of the search says about the row: what it is, and a line about it. */
  preview?: { kind: string; text: string };
  /** What the row stands for, for the side of the search to ask about. */
  subject?: TSearchSubject;
}

export const ANSWER = {
  wait: 'wait',
  stream: 'stream',
  ready: 'ready',
  noModel: 'noModel',
  failed: 'failed',
} as const;

/** The model's answer to a question in words. */
export type TSearchAnswer =
  | { k: typeof ANSWER.wait }
  /** The answer as far as the model has written it. */
  | { k: typeof ANSWER.stream; text: string }
  /** `sources` are the rows the answer rests on. */
  | { k: typeof ANSWER.ready; text: string; sources: readonly { row: ISearchRow; label: string }[] }
  /** No model is picked for the answers. */
  | { k: typeof ANSWER.noModel }
  | { k: typeof ANSWER.failed; message: string };

/** What makes the dialog the search of everything: the side with the selected row, the answer, settings in rows. */
export interface ISearchSmart {
  /** null when the words are not a question. */
  answer: TSearchAnswer | null;
  /** Asks the model about the words as they are (Tab). */
  onAsk: () => void;
  /** Changes a setting; resolves true when the machine took it. */
  onSetting: (key: string, value: string) => Promise<boolean>;
  onAddSource: () => void;
  onPickModel: () => void;
  /** What the side says about the selected row. */
  detail?: IDetailSource;
  /** What the side asked about while the search is open. */
  kept?: Map<string, ISearchDetail>;
  /** A link opens beside the chat on screen (its pane) and not in another tab of the browser. */
  beside?: boolean;
  /** Opens the row aside (⌘↵): beside the chat, or in a new tab of the browser. */
  onAside?: (row: ISearchRow) => void;
  /** Stops a chat's turn or wakes its agent; resolves true when the machine took it. */
  onChatAct?: (chatId: string, act: TDetailAct) => Promise<boolean>;
}

export interface ISearchGroup {
  id: string;
  label: string;
  rows: readonly ISearchRow[];
}

export interface ISearchDialogProps {
  open: boolean;
  onClose: () => void;
  query: string;
  onQuery: (query: string) => void;
  scopes: readonly ISearchScope[];
  scope: string;
  onScope: (scope: string) => void;
  /** The answer for the query; null while it is on its way. */
  results: readonly ISearchGroup[] | null;
  /** What shows before anything is typed. */
  recent: readonly ISearchRow[];
  /** Other words for the field and the box (the chat navigator uses this dialog); the search's own when absent. */
  words?: { title: string; placeholder: string };
  /** What shows before anything is typed instead of the recent (grouped as the results are); null while the list of
   *  the tab is on its way. */
  idle?: readonly ISearchGroup[] | null;
  /** What an empty tab says before anything is typed. */
  empty?: { title: string; hint: string };
  onPick: (row: ISearchRow) => void;
  /** The machine is still reading its older chats, so the messages found may not be all there are. */
  indexing?: boolean;
  /** The chat list did not come: why, and how to ask again. null while it is on its way or here. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  /** The words lit in the rows when they are not the query itself (the keywords of a question). */
  marks?: string;
  smart?: ISearchSmart;
}

const SAVED_MS = 1600;
const COPIED_MS = 1400;
const SKELETON_ROWS = 6;

const urlOf = (row: ISearchRow): string | null =>
  row.go && (row.go.k === ROW_GO.link || row.go.k === ROW_GO.review) ? row.go.url : null;

const chatOf = (row: ISearchRow): string | null => (row.subject?.k === SUBJECT.chat ? row.subject.id : null);

const openLabel = (row: ISearchRow): string => {
  if (row.go?.k === ROW_GO.setting || row.go?.k === ROW_GO.section) return t('next.search.actOpenSettings');
  if (row.go?.k === ROW_GO.newChat) return t('next.search.actCreate');
  if (row.target) return t('next.search.actOpenMessage');
  return t('next.search.actOpen');
};

/** What can be done with the selected row, the first by Enter. */
const actionsOf = (
  row: ISearchRow,
  detail: ISearchDetail | null,
  smart: ISearchSmart,
  mod: string,
): ISideAction[] => {
  const open = (key: string): ISideAction => ({
    id: SIDE_ACT.open,
    icon: row.go?.k === ROW_GO.setting ? 'settings' : 'corner-down-left',
    label: openLabel(row),
    key,
  });
  if (row.widget?.kind === ROW_WIDGET.toggle)
    return [
      {
        id: SIDE_ACT.widget,
        icon: 'corner-down-left',
        label: row.widget.on ? t('next.search.actOff') : t('next.search.actOn'),
        key: '↵',
      },
      open(`${mod}↵`),
    ];
  if (row.widget?.kind === ROW_WIDGET.pick)
    return [
      { id: SIDE_ACT.widget, icon: 'corner-down-left', label: t('next.search.actChoose'), key: '↵' },
      open(`${mod}↵`),
    ];
  const acts = [open('↵')];
  const link = urlOf(row) ?? detail?.link ?? null;
  if (smart.onAside && link !== null)
    acts.push({
      id: SIDE_ACT.aside,
      icon: urlOf(row) !== null && smart.beside ? 'panel-right' : 'external-link',
      label: urlOf(row) !== null && smart.beside ? t('next.search.actBeside') : t('next.search.actNewTab'),
      key: `${mod}↵`,
    });
  if (detail?.act && smart.onChatAct)
    acts.push({
      id: SIDE_ACT.chat,
      icon: detail.act === 'stop' ? 'square' : 'play',
      label: detail.act === 'stop' ? t('next.search.actStop') : t('next.search.actResume'),
      key: '',
    });
  if (link !== null) acts.push({ id: SIDE_ACT.copy, icon: 'copy', label: t('next.search.actCopy'), key: '' });
  return acts;
};

const NO_SELECTION = { key: '', id: null, at: 0 } as const;
const rowsOf = (group: ISearchGroup) => group.rows;
const idOfRow = (row: ISearchRow) => row.id;
const withRows = (group: ISearchGroup, rows: readonly ISearchRow[]): ISearchGroup => ({ ...group, rows });

/** The groups held in the layout the words showed first; exported for the tests. */
export const steadyGroups = (
  groups: readonly ISearchGroup[],
  layout: TShownLayout | null,
): readonly ISearchGroup[] => steadyLists(groups, layout, rowsOf, withRows, idOfRow);

const groupsShown = (
  typed: boolean,
  results: readonly ISearchGroup[] | null,
  recent: readonly ISearchRow[],
  idle?: readonly ISearchGroup[] | null,
): readonly ISearchGroup[] => {
  if (typed) return results ?? [];
  if (idle !== undefined) return idle ?? [];
  if (recent.length === 0) return [];
  return [{ id: 'recent', label: t('next.search.recent'), rows: recent }];
};

const Highlighted = ({ text, query }: { text: string; query: string }) => (
  <>
    {partsOf(text, query).map((part, i) => (
      <span key={i} data-hit={part.hit || undefined}>
        {part.text}
      </span>
    ))}
  </>
);

interface IPickProps {
  row: ISearchRow;
  widget: Extract<ISearchWidget, { kind: typeof ROW_WIDGET.pick }>;
  open: boolean;
  busy: boolean;
  onOpen: (open: boolean) => void;
  onPick: (value: string) => void;
}

/** The value of a setting with a list of values: a menu of them, opened from the row. */
const RowPick = ({ row, widget, open, busy, onOpen, onPick }: IPickProps) => {
  const actions = useMenuActions();
  return (
    <Menu.Root actionsRef={actions} open={open} onOpenChange={onOpen}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-srch__pick" disabled={busy} aria-label={row.title}>
            <span>{widget.label}</span>
            <Icon name="chevron-down" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={row.title} align="end" menu="search-pick">
        {widget.options.map((option) => (
          <Menu.Item
            key={option.value}
            className="hx-cmenu__item"
            role="menuitemradio"
            aria-checked={option.value === widget.value}
            onClick={() => {
              if (option.value !== widget.value) onPick(option.value);
            }}
          >
            <span className="hx-cmenu__txt">{option.label}</span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

/**
 * Search, ⌘K: one field, «where» scopes, results by group with the match lit, ↑↓ and Enter. An empty field shows what
 * to go on with; nothing found says so with a hint. With `smart` it is the search of everything: the side says what is
 * selected and what can be done with it, a setting is changed in its row, and a question gets the model's answer.
 */
const SearchDialog = ({
  open,
  onClose,
  query,
  onQuery,
  scopes,
  scope,
  onScope,
  results,
  recent,
  words,
  idle,
  empty,
  onPick,
  failed = null,
  indexing = false,
  marks,
  smart,
}: ISearchDialogProps) => {
  const searchKey = useKeyHint('global.search');
  const container = usePortalContainer();
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  /**
   * The selected row is held by its id: an answer that comes later moves nothing under the person, and if it
   * did, the selection would stay on the row they chose, not on its place. `at` is where it stood, for a row that went.
   */
  const [selected, setSelected] = useState<{ key: string; id: string | null; at: number }>(NO_SELECTION);
  /** The rows as this query showed them: a later answer (messages, the index, a source) adds below, moves nothing. */
  const [held, setHeld] = useState<{ key: string; layout: TShownLayout } | null>(null);
  /** The setting being saved, the one just saved, and the row whose menu of values is open. */
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [savedKey, setSavedKey] = useState<string | null>(null);
  const [pickOpen, setPickOpen] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  // The menu belongs to the row it was opened for: moving to another row closes it.
  const [sideMenu, setSideMenu] = useState<{ row: string | null; menu: ISideMenu }>({
    row: null,
    menu: SIDE_MENU_CLOSED,
  });
  const [chatBusy, setChatBusy] = useState(false);
  /** The last answer: it stays on screen while the next one is on its way, so typing does not blink the list. */
  const [last, setLast] = useState(results);
  if (results !== null && results !== last) setLast(results);
  const [noKept] = useState(() => new Map<string, ISearchDetail>());
  const typed = query.trim() !== '';
  const lit = marks ?? query;
  const stale = typed && results === null && last !== null;
  const holdKey = `${scope}\u0000${query.trim()}`;
  const natural = groupsShown(typed, results ?? last, recent, idle);
  const heldLayout = held !== null && held.key === holdKey ? held.layout : null;
  // The rows of the words before are still on screen while these are asked: they are not held for these words.
  const groups = stale ? natural : steadyGroups(natural, heldLayout);
  if (!open && held !== null) setHeld(null);
  else if (open && !stale) {
    const shown = layoutOf(groups, rowsOf, idOfRow);
    if (layoutKey(shown) !== layoutKey(heldLayout)) setHeld({ key: holdKey, layout: shown });
  }
  const starts = groups.map((_, i) => groups.slice(0, i).reduce((sum, group) => sum + group.rows.length, 0));
  const rows = groups.flatMap((group) => group.rows);
  const mark = selected.key === holdKey ? selected : NO_SELECTION;
  const found = mark.id === null ? -1 : rows.findIndex((row) => row.id === mark.id);
  const at = found >= 0 ? found : Math.min(mark.at, Math.max(0, rows.length - 1));
  const selectAt = (index: number) => setSelected({ key: holdKey, id: rows[index]?.id ?? null, at: index });
  const current = rows[at] ?? null;
  const currentGroup = groups.find(
    (group, i) => at >= (starts[i] ?? 0) && at < (starts[i] ?? 0) + group.rows.length,
  );
  const unavailable = failed !== null && results === null;
  const waiting = typed ? results === null && last === null : idle === null;
  const searching = useDelayedFlag(waiting && !unavailable);
  const nothing = typed && results !== null && rows.length === 0;
  const idleEmpty = !typed && idle !== undefined && idle !== null && rows.length === 0 && !unavailable;
  const elsewhere = scopes[0]?.count ?? 0;
  const mod = isMac() ? '⌘' : 'Ctrl ';
  const answer = smart?.answer ?? null;
  const rowDetail = useRowDetail(smart ? current : null, smart?.detail, smart?.kept ?? noKept);
  const sideActions = smart && current ? actionsOf(current, rowDetail?.detail ?? null, smart, mod) : [];

  useEffect(() => {
    if (savedKey === null) return;
    const timer = setTimeout(() => setSavedKey(null), SAVED_MS);
    return () => clearTimeout(timer);
  }, [savedKey]);
  useEffect(() => {
    if (copied === null) return;
    const timer = setTimeout(() => setCopied(null), COPIED_MS);
    return () => clearTimeout(timer);
  }, [copied]);

  const move = (delta: number) => {
    if (rows.length === 0) return;
    const next = (at + delta + rows.length) % rows.length;
    selectAt(next);
    listRef.current?.querySelector(`[data-row="${next}"]`)?.scrollIntoView({ block: 'nearest' });
  };

  /** The setting is changed on the machine first; the row shows the new value when the machine took it. */
  const change = (row: ISearchRow, value: string) => {
    if (!smart || row.go?.k !== ROW_GO.setting || busyKey !== null) return;
    const key = row.go.key;
    setBusyKey(key);
    void smart.onSetting(key, value).then((taken) => {
      setBusyKey(null);
      if (taken) setSavedKey(key);
    });
  };

  const act = (row: ISearchRow, id: TSideAct) => {
    if (id === SIDE_ACT.copy) {
      const url = urlOf(row) ?? rowDetail?.detail.link ?? null;
      if (url !== null) void navigator.clipboard.writeText(url).then(() => setCopied(row.id));
      return;
    }
    if (id === SIDE_ACT.widget && smart && row.widget?.kind === ROW_WIDGET.toggle) {
      change(row, row.widget.on ? SETTING_OFF : SETTING_ON);
      return;
    }
    if (id === SIDE_ACT.widget && smart && row.widget?.kind === ROW_WIDGET.pick) {
      setPickOpen(row.id);
      return;
    }
    if (id === SIDE_ACT.aside && smart?.onAside) {
      smart.onAside(row);
      return;
    }
    if (id === SIDE_ACT.chat) {
      const chat = chatOf(row);
      const what = rowDetail?.detail.act;
      if (!smart?.onChatAct || chat === null || !what || chatBusy) return;
      setChatBusy(true);
      void smart.onChatAct(chat, what).then((taken) => {
        setChatBusy(false);
        // The chat's state changed: what the side kept of it is asked again.
        if (taken) smart.kept?.delete(row.id);
      });
      return;
    }
    onPick(row);
  };

  const stepScope = (delta: number) => {
    const index = scopes.findIndex((item) => item.id === scope);
    const next = scopes[(index + delta + scopes.length) % scopes.length];
    if (!next) return;
    setSelected(NO_SELECTION);
    onScope(next.id);
    listRef.current
      ?.closest('.hx-srch__box')
      ?.querySelector(`[data-scope="${CSS.escape(next.id)}"]`)
      ?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  };

  const pickItem = (item: IDetailItem) => {
    if (item.go)
      onPick({ id: item.id, kind: 'link', icon: item.icon, title: item.title, meta: '', go: item.go });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      move(1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      move(-1);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      if (!current) return;
      const withMod = event.metaKey || event.ctrlKey;
      const withWidget = smart !== undefined && current.widget !== undefined;
      if (withWidget) act(current, withMod ? SIDE_ACT.open : SIDE_ACT.widget);
      else if (withMod && sideActions.some((action) => action.id === SIDE_ACT.aside))
        act(current, SIDE_ACT.aside);
      else act(current, SIDE_ACT.open);
    } else if (event.key === 'Tab' && smart && typed && !event.shiftKey) {
      // Tab asks the model about the words; with nothing typed, and with Shift, it walks the tabs.
      event.preventDefault();
      smart.onAsk();
    } else if (event.key === 'Tab' && scopes.length > 0) {
      event.preventDefault();
      stepScope(event.shiftKey && !(smart && typed) ? -1 : 1);
    } else if ((event.key === 'ArrowLeft' || event.key === 'ArrowRight') && !typed && scopes.length > 1) {
      // The field is empty, so the arrows have no caret to move: they walk the tabs.
      event.preventDefault();
      stepScope(event.key === 'ArrowLeft' ? -1 : 1);
    }
  };

  const select = (row: ISearchRow) => {
    const index = rows.findIndex((item) => item.id === row.id);
    if (index < 0) return;
    selectAt(index);
    listRef.current?.querySelector(`[data-row="${index}"]`)?.scrollIntoView({ block: 'nearest' });
  };

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Portal container={container}>
        <div className="hx-srch">
          <Dialog.Backdrop className="hx-srch__scrim" />
          <Dialog.Popup
            className="hx-srch__box"
            data-wide={smart !== undefined || undefined}
            aria-label={words?.title ?? t('next.search.title')}
            initialFocus={inputRef}
          >
            <label className="hx-srch__field">
              <Icon name="search" />
              <input
                ref={inputRef}
                type="search"
                placeholder={words?.placeholder ?? t('next.search.placeholder')}
                autoComplete="off"
                enterKeyHint="search"
                value={query}
                onChange={(event) => {
                  setSelected(NO_SELECTION);
                  onQuery(event.target.value);
                }}
                onKeyDown={onKeyDown}
              />
              {typed && (
                <button
                  type="button"
                  className="hx-srch__clear"
                  aria-label={t('next.search.clear')}
                  onClick={() => {
                    onQuery('');
                    inputRef.current?.focus();
                  }}
                >
                  <Icon name="circle-x" size="sm" />
                </button>
              )}
              <Dialog.Close className="hx-srch__cancel">{t('next.search.cancel')}</Dialog.Close>
            </label>
            {scopes.length > 0 && (
              <div className="hx-srch__scopes" role="tablist" aria-label={t('next.search.where')}>
                {scopes.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="tab"
                    className="hx-srch__scope"
                    data-scope={item.id}
                    aria-selected={item.id === scope}
                    title={item.hint}
                    onClick={() => {
                      setSelected(NO_SELECTION);
                      onScope(item.id);
                    }}
                  >
                    {item.label}
                    {typed && item.count !== undefined && item.count > 0 && (
                      <span className="hx-srch__count">{item.count}</span>
                    )}
                  </button>
                ))}
                {smart && (
                  <button
                    type="button"
                    className="hx-srch__scope hx-srch__scope--add"
                    title={t('next.search.addSourceHint')}
                    onClick={smart.onAddSource}
                  >
                    <Icon name="plug" size="sm" />
                    {t('next.search.addSource')}
                  </button>
                )}
              </div>
            )}
            <div className="hx-srch__body">
              <div
                className="hx-srch__list"
                ref={listRef}
                role="listbox"
                aria-label={t('next.search.results')}
              >
                {answer && (
                  <div className="hx-srch__ans" role="status">
                    <div className="hx-srch__ans-head">
                      <Icon name="sparkles" size="sm" />
                      {t('next.search.answerTitle')}
                    </div>
                    {answer.k === ANSWER.wait && (
                      <div className="hx-srch__ans-wait">
                        <Icon name="loader-circle" size="sm" />
                        {t('next.search.answerWait')}
                      </div>
                    )}
                    {answer.k === ANSWER.stream && (
                      <div className="hx-srch__ans-text" data-writing="true">
                        {answer.text}
                      </div>
                    )}
                    {answer.k === ANSWER.ready && (
                      <>
                        <div className="hx-srch__ans-text">{answer.text}</div>
                        {answer.sources.length > 0 && (
                          <div className="hx-srch__ans-srcs">
                            {answer.sources.map((source) => (
                              <button
                                key={source.row.id}
                                type="button"
                                className="hx-srch__ans-src"
                                onClick={() => select(source.row)}
                              >
                                <Icon name={source.row.icon} size="sm" />
                                <span>{source.label}</span>
                              </button>
                            ))}
                          </div>
                        )}
                      </>
                    )}
                    {answer.k === ANSWER.noModel && smart && (
                      <div className="hx-srch__ans-wait">
                        <span>{t('next.search.answerNoModel')}</span>
                        <button type="button" className="hx-srch__ans-src" onClick={smart.onPickModel}>
                          {t('next.search.answerPickModel')}
                        </button>
                      </div>
                    )}
                    {answer.k === ANSWER.failed && <div className="hx-srch__ans-wait">{answer.message}</div>}
                  </div>
                )}
                {searching && (
                  <div className="hx-srch__sks" role="status" aria-label={t('next.search.searching')}>
                    {Array.from({ length: SKELETON_ROWS }, (_, i) => (
                      <div key={i} className="hx-srch__skrow">
                        <Skeleton className="hx-srch__sk-ic" />
                        <Skeleton className="hx-srch__sk-line" />
                      </div>
                    ))}
                  </div>
                )}
                {groups.map((group, groupIndex) => (
                  <div
                    key={group.id}
                    className="hx-srch__group"
                    role="group"
                    aria-label={group.label}
                    data-stale={stale || undefined}
                  >
                    <div className="hx-srch__label">{group.label}</div>
                    {group.rows.map((row, rowIndex) => {
                      const mine = (starts[groupIndex] ?? 0) + rowIndex;
                      const settingKey = row.go?.k === ROW_GO.setting ? row.go.key : null;
                      const widget = smart ? row.widget : undefined;
                      return (
                        <div
                          key={row.id}
                          role="option"
                          tabIndex={-1}
                          aria-selected={mine === at}
                          className="hx-srch__row"
                          data-row={mine}
                          data-sel={mine === at}
                          data-kind={row.kind}
                          onClick={() => {
                            // A row with a control is changed by its control; the row itself only takes the selection.
                            if (widget) selectAt(mine);
                            else onPick(row);
                          }}
                          onPointerMove={() => {
                            if (mine !== at) selectAt(mine);
                          }}
                          onContextMenu={(event) => {
                            // A right click selects the row and opens what can be done with it, where it was clicked.
                            if (!smart) return;
                            event.preventDefault();
                            selectAt(mine);
                            setSideMenu({
                              row: row.id,
                              menu: { open: true, at: { x: event.clientX, y: event.clientY } },
                            });
                          }}
                        >
                          <span className="hx-srch__ic">
                            <Icon name={row.icon} size="sm" />
                          </span>
                          <span className="hx-srch__text">
                            <span className="hx-srch__title" title={row.title}>
                              <Highlighted text={row.title} query={lit} />
                            </span>
                            {row.snippet && (
                              <span className="hx-srch__snip">
                                <Highlighted text={row.snippet} query={lit} />
                              </span>
                            )}
                          </span>
                          {settingKey !== null && savedKey === settingKey && (
                            <span className="hx-srch__saved">{t('next.search.saved')}</span>
                          )}
                          {widget?.kind === ROW_WIDGET.toggle && (
                            <Switch
                              label={row.title}
                              checked={widget.on}
                              disabled={busyKey !== null}
                              onChange={(on) => change(row, on ? SETTING_ON : SETTING_OFF)}
                            />
                          )}
                          {widget?.kind === ROW_WIDGET.pick && (
                            <RowPick
                              row={row}
                              widget={widget}
                              open={pickOpen === row.id}
                              busy={busyKey !== null}
                              onOpen={(next) => setPickOpen(next ? row.id : null)}
                              onPick={(value) => change(row, value)}
                            />
                          )}
                          {!widget && <span className="hx-srch__meta">{row.meta}</span>}
                          {!widget && !smart && (
                            <Icon name="corner-down-left" size="sm" className="hx-srch__go" />
                          )}
                        </div>
                      );
                    })}
                  </div>
                ))}
                {typed && indexing && rows.length > 0 && (
                  <div className="hx-srch__note">{t('next.search.indexing')}</div>
                )}
                {nothing && (
                  <div className="hx-srch__none">
                    <b>{t('next.search.nothing')}</b>
                    <span>{t('next.search.nothingHint')}</span>
                    {indexing && <span>{t('next.search.indexing')}</span>}
                    {scope !== scopes[0]?.id && elsewhere > 0 && (
                      <button
                        type="button"
                        className="hx-srch__none-go"
                        onClick={() => {
                          const all = scopes[0];
                          if (all) onScope(all.id);
                        }}
                      >
                        {t('next.search.nothingElsewhere', { count: elsewhere })}
                      </button>
                    )}
                  </div>
                )}
                {idleEmpty && empty && (
                  <div className="hx-srch__none">
                    <b>{empty.title}</b>
                    <span>{empty.hint}</span>
                  </div>
                )}
                {unavailable && (
                  <LoadFailed
                    size={LOAD_FAILED_SIZE.pane}
                    title={t('next.load.search')}
                    reason={failed.reason}
                    onRetry={failed.onRetry}
                  />
                )}
                {!typed && recent.length === 0 && idle === undefined && !unavailable && (
                  <div className="hx-srch__none">
                    <span>{t('next.search.start')}</span>
                  </div>
                )}
              </div>
              {smart && current && (
                <SearchSide
                  key={current.id}
                  row={current}
                  kind={current.preview?.kind ?? currentGroup?.label ?? ''}
                  lit={lit}
                  detail={rowDetail}
                  actions={sideActions}
                  copied={copied === current.id}
                  busy={chatBusy}
                  menu={sideMenu.row === current.id ? sideMenu.menu : SIDE_MENU_CLOSED}
                  onMenu={(menu) => setSideMenu({ row: current.id, menu })}
                  onAct={(id) => act(current, id)}
                  onItem={pickItem}
                />
              )}
            </div>
            <div className="hx-srch__foot">
              <span>
                <kbd>↑</kbd>
                <kbd>↓</kbd>
                {t('next.search.keySelect')}
              </span>
              <span>
                <kbd>↵</kbd>
                {t('next.search.keyOpen')}
              </span>
              {smart && typed && (
                <span>
                  <kbd>Tab</kbd>
                  {t('next.search.keyAsk')}
                </span>
              )}
              {smart && typed && (
                <span>
                  <kbd>⇧</kbd>
                  <kbd>Tab</kbd>
                  {t('next.search.keyScope')}
                </span>
              )}
              {smart && !typed && scopes.length > 1 && (
                <span>
                  <kbd>←</kbd>
                  <kbd>→</kbd>
                  {t('next.search.keyScope')}
                </span>
              )}
              {!smart && scopes.length > 0 && (
                <span>
                  <kbd>Tab</kbd>
                  {t('next.search.where')}
                </span>
              )}
              <span className="hx-spacer" />
              {searchKey && (
                <span>
                  <kbd>{searchKey}</kbd>
                </span>
              )}
            </div>
          </Dialog.Popup>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
};

export default SearchDialog;
