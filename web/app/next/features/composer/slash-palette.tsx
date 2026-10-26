// The palette over the field (the design's .hx-ph, as «/» opens it): a word that starts with «/» (at the start of the
// field or after a space) lists the chat's settings, the agent's commands and skills, its MCP servers and the quick
// prompts; «$» lists Codex's skills; «@» walks the chat's folder for a file. ↑↓ choose, Tab or Enter complete, Esc
// closes. «/model» and «/effort» turn the field into the list of models (or depths): they are changed through the
// pill's own switch, nothing is typed into the agent. A word the palette has no row for goes to the agent as text.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { modelOf, type IPickerAgent } from '@/next/features/composer/agents';
import type { IModelPickerProps } from '@/next/features/composer/model-picker';
import type { IQuickPrompt } from '@/next/features/composer/prompts';
import type { IChatPick } from '@/next/features/composer/chat-sources';
import { highlighted } from '@/next/features/composer/slash-match';
import {
  buildRows,
  chatRows,
  clampSelection,
  detectSub,
  detectTrigger,
  effortRows,
  fileRows,
  GROUP,
  isTyped,
  modelRows,
  moveSelection,
  removeWord,
  replaceWord,
  splitMention,
  SUB,
  TRIGGER_CHAR,
  type IMcpRef,
  type IRowList,
  type ISlashActions,
  type ISlashRow,
  type ITrigger,
  type TGroup,
  type TSub,
} from '@/next/features/composer/slash-model';
import type { ICatalogItem } from '@/next/features/settings/api';
import type { FileEntry, MessageRef, Provider } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag, type TPresenceState } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';

/** The panel's exit, as long as hx-anns-out in composer.css. */
const EXIT_MS = 180;

/** What the screen gives the palette to list: the agent's catalog, its servers, what the chat can do, its folder. */
export interface ISlashSource {
  provider: Provider;
  /** Asked when the palette first opens, so the catalog is not read for a person who never types «/». */
  wake: () => void;
  /** null: still being read. */
  catalog: readonly ICatalogItem[] | null;
  mcp: readonly IMcpRef[];
  actions: ISlashActions;
  /** The chat is not started yet (the start page): only what may be a first message is offered. */
  starting: boolean;
  /** Names the folder the entries belong to (the chat); a new one forgets what was listed. */
  filesScope: string;
  /** The entries of a folder of the chat's (relative to it); null where the chat has none to list (the start page). */
  listFolder: ((dir: string, signal: AbortSignal) => Promise<readonly FileEntry[]>) | null;
  /** The chats «@» offers beside the files, for the words typed; null where a message cannot point at a chat. */
  findChats?: ((query: string, signal: AbortSignal) => Promise<readonly IChatPick[]>) | null;
  /** A chat is picked: the message points at it from now on. */
  onChat?: (ref: MessageRef) => void;
}

interface ISlashOptions {
  value: string;
  onChange: (value: string) => void;
  fieldRef: RefObject<HTMLTextAreaElement | null>;
  /** The pill's props: «/model» and «/effort» change the choice through them. */
  picker: IModelPickerProps | null;
  slash: ISlashSource | null;
  quick: readonly IQuickPrompt[];
  /** Off while the field is not the thing to type into (the recording bar). */
  enabled: boolean;
}

export interface ISlashPalette {
  panel: ReactNode;
  /** Gives the key to the palette when it takes it; true: the key is spent. */
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => boolean;
  /** The text or the caret of the field changed. */
  sync: (text: string, caret: number) => void;
  close: () => void;
}

interface IView {
  list: IRowList;
  /** Loaded nothing yet: the agent's own commands are being read. */
  loading: boolean;
  sub: TSub | null;
  trigger: ITrigger | null;
  /** What the choice among models (depths) says it cannot do now. */
  note: string | null;
  /** The sub list changes nothing while it is true. */
  frozen: boolean;
  /** The folder could not be listed. */
  failed: boolean;
}

/** What the view shows, as one line: the panel is kept in its last shape while it leaves. */
const signature = (view: IView): string =>
  JSON.stringify([
    view.list.rows.map((row) => [row.id, row.ranges, row.current]),
    view.list.more,
    view.loading,
    view.sub,
    view.note,
    view.failed,
  ]);

const pickerAgent = (picker: IModelPickerProps | null): IPickerAgent | null =>
  picker
    ? (picker.agents.find((a) => a.provider === picker.value.provider) ?? picker.agents[0] ?? null)
    : null;

interface IFolders {
  scope: string;
  /** dir → its entries; null: it could not be read. */
  dirs: Record<string, readonly FileEntry[] | null>;
}

const NO_FOLDERS: IFolders = { scope: '', dirs: {} };

/** The chats found for a word, kept with the word: a list for another word is not the answer. */
interface IChatsFound {
  scope: string;
  query: string;
  picks: readonly IChatPick[];
}

/** The chats are asked of the machine this long after the last key. */
const CHATS_DEBOUNCE_MS = 160;

export const useSlashPalette = ({
  value,
  onChange,
  fieldRef,
  picker,
  slash,
  quick,
  enabled,
}: ISlashOptions): ISlashPalette => {
  const [caret, setCaret] = useState(0);
  // The text the person sent away with Esc (or by leaving the field): the palette stays shut until the text changes.
  const [dismissed, setDismissed] = useState<string | null>(null);
  const [picked, setPicked] = useState({ key: '', index: 0 });
  const [folders, setFolders] = useState<IFolders>(NO_FOLDERS);
  const [chats, setChats] = useState<IChatsFound | null>(null);
  const [kept, setKept] = useState<{ sig: string; view: IView } | null>(null);
  const pendingCaret = useRef<{ text: string; caret: number } | null>(null);

  const live = enabled && slash !== null && dismissed !== value;
  const agent = pickerAgent(picker);
  const subs: TSub[] = [];
  if (agent && agent.models.length > 0) subs.push(SUB.model);
  if (agent && picker && (modelOf(agent, picker.value.model)?.efforts.length ?? 0) > 0) subs.push(SUB.effort);

  const sub = live ? detectSub(value, subs) : null;
  const trigger =
    live && slash && !sub
      ? detectTrigger(value, Math.min(caret, value.length), {
          skills: slash.provider === 'codex',
          files: slash.listFolder !== null || !!slash.findChats,
        })
      : null;

  const dir = trigger?.char === TRIGGER_CHAR.file ? splitMention(trigger.query).dir : null;
  const scope = slash?.filesScope ?? '';
  const known = folders.scope === scope ? folders.dirs : NO_FOLDERS.dirs;
  const listFolder = slash?.listFolder ?? null;
  const asked = dir !== null && dir in known;
  useEffect(() => {
    if (dir === null || !listFolder || asked) return;
    const ctl = new AbortController();
    listFolder(dir, ctl.signal).then(
      (entries) =>
        setFolders((now) => ({ scope, dirs: { ...(now.scope === scope ? now.dirs : {}), [dir]: entries } })),
      () => {
        if (!ctl.signal.aborted)
          setFolders((now) => ({ scope, dirs: { ...(now.scope === scope ? now.dirs : {}), [dir]: null } }));
      },
    );
    return () => ctl.abort();
  }, [dir, listFolder, asked, scope]);
  // A folder is read again when the word is typed again: the chat's files change while it works.
  const mentioning = trigger?.char === TRIGGER_CHAR.file;
  if (!mentioning && folders !== NO_FOLDERS) setFolders(NO_FOLDERS);

  // The chats «@» offers: asked for the word's name part when it is not a path; the answer is kept for that word.
  const chatQuery =
    trigger?.char === TRIGGER_CHAR.file && !trigger.query.includes('/') ? trigger.query : null;
  const findChats = slash?.findChats ?? null;
  useEffect(() => {
    if (chatQuery === null || !findChats) return;
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      findChats(chatQuery, ctl.signal).then(
        (picks) => setChats({ scope, query: chatQuery, picks }),
        () => {
          if (!ctl.signal.aborted) setChats({ scope, query: chatQuery, picks: [] });
        },
      );
    }, CHATS_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
  }, [chatQuery, findChats, scope]);
  const chatPicks =
    chatQuery !== null && chats?.scope === scope && chats.query === chatQuery ? chats.picks : null;

  const wanted = sub !== null || trigger !== null;
  const wake = slash?.wake;
  useEffect(() => {
    if (wanted) wake?.();
  }, [wanted, wake]);

  const actions = slash?.actions;
  let view: IView | null = null;
  if (sub && agent && picker) {
    const rows =
      sub.sub === SUB.model
        ? modelRows(agent, picker.value, sub.query)
        : effortRows(agent, picker.value, sub.query);
    if (rows.length > 0)
      view = {
        list: { rows, more: 0 },
        loading: false,
        sub: sub.sub,
        trigger: null,
        note: picker.blocked ?? null,
        frozen: !!picker.blocked || !!picker.busy,
        failed: false,
      };
  } else if (trigger && slash && actions) {
    let list: IRowList;
    let failed = false;
    if (trigger.char === TRIGGER_CHAR.file) {
      const entries = dir !== null ? known[dir] : undefined;
      failed = entries === null;
      list = entries ? fileRows(entries, splitMention(trigger.query).name) : { rows: [], more: 0 };
      if (chatPicks) list = { ...list, rows: [...list.rows, ...chatRows(chatPicks, trigger.query)] };
    } else {
      list = buildRows({
        trigger,
        provider: slash.provider,
        catalog: slash.catalog,
        mcp: slash.mcp,
        quick,
        starting: slash.starting,
        available: { model: subs.includes(SUB.model), effort: subs.includes(SUB.effort), actions },
      });
    }
    const loading = trigger.char !== TRIGGER_CHAR.file && slash.catalog === null;
    if (list.rows.length > 0 || loading || failed)
      view = { list, loading, sub: null, trigger, note: null, frozen: false, failed };
  }

  const sig = view ? signature(view) : null;
  if (view && sig !== null && kept?.sig !== sig) setKept({ sig, view });
  const shown = view ?? kept?.view ?? null;
  const { mounted, state } = usePresentFlag(view !== null, EXIT_MS);

  const key = sub
    ? `sub:${sub.sub}:${sub.query}`
    : trigger
      ? `${trigger.char}${trigger.start}:${trigger.query}`
      : '';
  const rows = view?.list.rows ?? [];
  const selected = clampSelection(picked.key === key ? picked.index : 0, rows.length);

  // The caret goes after what a row put into the field, once the field has that text.
  useLayoutEffect(() => {
    const field = fieldRef.current;
    const pending = pendingCaret.current;
    if (!field || !pending || field.value !== pending.text) return;
    pendingCaret.current = null;
    field.setSelectionRange(pending.caret, pending.caret);
  });

  const put = (next: { text: string; caret: number }) => {
    pendingCaret.current = next;
    setCaret(next.caret);
    onChange(next.text);
    fieldRef.current?.focus();
  };

  const choose = (row: ISlashRow) => {
    const { action } = row;
    if (action.k === 'choose') {
      if (!picker || picker.busy) return;
      if (picker.blocked) {
        toast(picker.blocked, { tone: 'info' });
        return;
      }
      // The same switch as the pill's: a change waits for the agent's answer and a refusal says why.
      picker.onChange(action.choice);
      put({ text: '', caret: 0 });
      return;
    }
    if (!trigger) return;
    if (action.k === 'chat') {
      put(removeWord(value, trigger.start, trigger.end));
      slash?.onChat?.(action.ref);
    } else if (action.k === 'sub') put(replaceWord(value, trigger.start, trigger.end, `/${action.sub} `));
    else if (action.k === 'insert' || action.k === 'dir')
      put(replaceWord(value, trigger.start, trigger.end, action.text));
    else {
      put(removeWord(value, trigger.start, trigger.end));
      actions?.[action.run]?.();
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!view || !mounted || event.nativeEvent.isComposing) return false;
    const plain = !event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey;
    if (event.key === 'Escape') {
      event.preventDefault();
      setDismissed(value);
      return true;
    }
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && plain && rows.length > 0) {
      event.preventDefault();
      setPicked({ key, index: moveSelection(selected, event.key === 'ArrowDown' ? 1 : -1, rows.length) });
      return true;
    }
    if (!plain || rows.length === 0) return false;
    const row = rows[selected];
    if (event.key === 'Tab') {
      event.preventDefault();
      choose(row);
      return true;
    }
    // A name typed in full and sent is a command as typed: it goes to the agent, the palette does not complete it.
    if (event.key === 'Enter' && !(trigger && isTyped(row, trigger.query))) {
      event.preventDefault();
      choose(row);
      return true;
    }
    return false;
  };

  const sync = (text: string, at: number) => {
    pendingCaret.current = null;
    setCaret(at);
    // The text moved on from where it was sent away: the palette may open again.
    if (dismissed !== null && dismissed !== text) setDismissed(null);
  };

  const close = () => setDismissed(value);

  const panel =
    mounted && shown ? (
      <SlashPanel
        view={shown}
        state={state}
        selected={selected}
        onHover={(index) => setPicked({ key, index })}
        onPick={choose}
      />
    ) : null;

  return { panel, onKeyDown, sync, close };
};

const GROUP_KEY: Record<TGroup, string> = {
  [GROUP.setting]: 'next.slash.group.setting',
  [GROUP.command]: 'next.slash.group.command',
  [GROUP.skill]: 'next.slash.group.skill',
  [GROUP.mcp]: 'next.slash.group.mcp',
  [GROUP.quick]: 'next.slash.group.quick',
  [GROUP.file]: 'next.slash.group.file',
  [GROUP.chat]: 'next.slash.group.chat',
};

interface ISlashPanelProps {
  view: IView;
  state: TPresenceState;
  selected: number;
  onHover: (index: number) => void;
  onPick: (row: ISlashRow) => void;
}

const Marked = ({ text, row }: { text: string; row: ISlashRow }) => (
  <>
    {highlighted(text, row.ranges).map((part, index) =>
      part.hit ? (
        <mark key={index} className="hx-slash__hit">
          {part.text}
        </mark>
      ) : (
        part.text
      ),
    )}
  </>
);

const SlashPanel = ({ view, state, selected, onHover, onPick }: ISlashPanelProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const { rows } = view.list;

  // The chosen row stays in sight when the list scrolls.
  useEffect(() => {
    rootRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' });
  }, [selected]);

  // The field keeps the focus while the list is clicked.
  const keepFocus = (event: MouseEvent) => event.preventDefault();

  const label = view.sub ? t(`next.slash.pick.${view.sub}`) : null;
  return (
    <div
      ref={rootRef}
      className="hx-slash"
      data-state={state}
      data-frozen={view.frozen || undefined}
      role="listbox"
      aria-label={label ?? t('next.slash.label')}
      onMouseDown={keepFocus}
    >
      {label && (
        <div className="hx-slash__head">
          <span>{label}</span>
          {view.note && <span className="hx-slash__note">{view.note}</span>}
        </div>
      )}
      {rows.map((row, index) => {
        const first = index === 0 || rows[index - 1].group !== row.group;
        const heading = !view.sub && first ? t(GROUP_KEY[row.group]) : null;
        const setPlace = (element: HTMLDivElement | null) =>
          element?.style.setProperty('--j', String(Math.min(index, 12)));
        return (
          <div key={row.id}>
            {heading && <div className="hx-slash__label">{heading}</div>}
            <div
              ref={setPlace}
              className="hx-slash__row"
              role="option"
              aria-selected={index === selected}
              data-current={row.current || undefined}
              tabIndex={-1}
              onMouseMove={() => index !== selected && onHover(index)}
              onClick={() => onPick(row)}
            >
              <Icon name={row.icon} size="sm" />
              <span className="hx-slash__name">
                {row.prefix}
                <Marked text={row.name} row={row} />
              </span>
              {row.hint && <span className="hx-slash__arg">{row.hint}</span>}
              <span className="hx-slash__desc">{row.description}</span>
              {row.current && <Icon name="check" size="sm" className="hx-slash__now" />}
              <span className="hx-slash__src">{row.source}</span>
            </div>
          </div>
        );
      })}
      {view.loading && <div className="hx-slash__empty">{t('next.slash.loading')}</div>}
      {view.failed && <div className="hx-slash__empty">{t('next.slash.filesFailed')}</div>}
      {view.list.more > 0 && (
        <div className="hx-slash__more">{t('next.slash.more', { n: view.list.more })}</div>
      )}
      <div className="hx-slash__foot">
        <span>
          <kbd>↑</kbd>
          <kbd>↓</kbd>
          {t('next.slash.choose')}
        </span>
        <span>
          <kbd>Tab</kbd>
          {view.sub ? t('next.slash.apply') : t('next.slash.complete')}
        </span>
        <span>
          <kbd>Esc</kbd>
          {t('next.slash.close')}
        </span>
      </div>
    </div>
  );
};
