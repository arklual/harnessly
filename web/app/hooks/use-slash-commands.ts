import { useCallback, useId, useMemo, useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import { toast } from 'sonner';
import { LISTBOX_ACTION, useTwListboxNav, type ITwOption } from '@/components/agents/ui';
import useCommandCatalog, {
  refreshCommandCatalogs,
  type IUseCommandCatalog,
} from '@/hooks/use-command-catalog';
import { appFetch } from '@/lib/app-path';
import { BUILTIN_COMMANDS } from '@/lib/hl-command-catalog/items';
import type { ICommandItem } from '@/lib/hl-command-catalog/types';
import {
  completeCommand,
  needsArgument,
  parseCommandQuery,
  rankCommands,
  type IRankedCommands,
  type TCommandQuery,
} from '@/lib/hl-command-filter';
import type { IApplied, ITuiMenu, TRunBody, TRunError } from '@/lib/hl-command-run';
import {
  CLAUDE_LAUNCH_MODELS,
  OPEN_LAUNCH_PICKER_EVENT,
  effortLabel,
  type IOpenLaunchPickerDetail,
} from '@/lib/hl-launch-options';
import { PENDING_SLASH_COMMAND_EVENT, type IPendingSlashCommandDetail } from '@/types/timeline';
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

const COARSE_QUERY = '(pointer: coarse)';
const subscribeCoarse = (listener: () => void) => {
  if (typeof window === 'undefined' || !window.matchMedia) return () => {};
  const media = window.matchMedia(COARSE_QUERY);
  media.addEventListener('change', listener);
  return () => media.removeEventListener('change', listener);
};
const readCoarse = () => typeof window !== 'undefined' && !!window.matchMedia?.(COARSE_QUERY).matches;
const noCoarseOnServer = () => false;

/** On a touch screen a tap only completes the command; sending stays on the send button. */
const useCoarsePointer = () => useSyncExternalStore(subscribeCoarse, readCoarse, noCoarseOnServer);

export type TPickVia = 'enter' | 'tab' | 'pointer';

export interface IUseSlashCommandsOptions {
  provider: TAgentProvider | null;
  machineId?: string;
  cwd: string | null;
  value: string;
  setValue: (value: string) => void;
  /** The composer has focus (or had it): the catalog loads from then on. */
  active: boolean;
  /** Welcome composer: only commands that may start a conversation. */
  welcome?: boolean;
  /** A command picked to run now (Enter or a click on a command without a required argument). */
  onRun: (item: ICommandItem, line: string) => void;
  /** Keeps the list closed (a first command waiting for the agent, a command running). */
  disabled?: boolean;
}

export interface IUseSlashCommands {
  listId: string;
  open: boolean;
  query: TCommandQuery | null;
  ranked: IRankedCommands | null;
  options: ITwOption[] | null;
  itemByValue: (value: string) => ICommandItem | null;
  activeValue: string | null;
  setActiveValue: (value: string | null) => void;
  activeDescendant: string | undefined;
  catalog: IUseCommandCatalog;
  showHidden: boolean;
  setShowHidden: (value: boolean) => void;
  /** "/compact [instructions]" under the field while arguments are typed. */
  argumentHint: string | null;
  /** Called first on the editor's keydown; true when the list consumed the key. */
  handleKeyDown: (event: KeyboardEvent<HTMLElement>) => boolean;
  pick: (item: ICommandItem, via: TPickVia) => void;
  close: () => void;
  /** Opens the full list (`/help`). */
  openAll: () => void;
  /** The catalog entry of a typed command, or null when unknown or not loaded yet. */
  resolve: (name: string, trigger?: '/' | '$') => ICommandItem | null;
}

/**
 * The slash-command list of a composer — when it opens, ranking, keyboard (↑↓, PgUp/PgDn, Tab, Enter,
 * Esc closes the list before anything else), pointer and touch behaviour. Shared by WebInputBar and the welcome slot.
 */
const useSlashCommands = ({
  provider,
  machineId,
  cwd,
  value,
  setValue,
  active,
  welcome = false,
  onRun,
  disabled = false,
}: IUseSlashCommandsOptions): IUseSlashCommands => {
  const listId = useId();
  const coarse = useCoarsePointer();
  const [dismissedAt, setDismissedAt] = useState<string | null>(null);
  const [showHidden, setShowHidden] = useState(false);
  const query = provider ? parseCommandQuery(value, provider) : null;
  const catalog = useCommandCatalog(provider, machineId, cwd, active || query !== null);
  const items = catalog.catalog?.items ?? null;
  const listQuery = query?.kind === 'list' ? query : null;
  const open = listQuery !== null && dismissedAt !== value && !disabled;

  // Plain derivations: the React Compiler memoizes them.
  const ranked =
    listQuery && items
      ? rankCommands(items, listQuery.query, { trigger: listQuery.trigger, showHidden, welcome })
      : null;
  const byId = new Map((ranked?.items ?? []).map((item) => [item.id, item]));
  let options: ITwOption[] | null = null;
  if (open && ranked) {
    options = ranked.items.map((item) => ({ value: item.id, label: `${item.trigger}${item.name}` }));
  } else if (open && catalog.error) {
    options = [];
  }
  const nav = useTwListboxNav({ listId, options });

  const resolve = useCallback(
    (name: string, trigger: '/' | '$' = '/') => {
      const wanted = name.toLowerCase();
      const matches = (items ?? []).filter(
        (item) =>
          item.trigger === trigger &&
          (item.name.toLowerCase() === wanted ||
            item.aliases.some((alias) => alias.toLowerCase() === wanted)),
      );
      const found = matches.find((item) => item.source === 'builtin') ?? matches[0];
      if (found || !provider) return found ?? null;
      // Before the catalog arrives the built-ins still decide how a typed command runs (model chip, blocked…).
      return (
        BUILTIN_COMMANDS[provider].find(
          (item) => item.trigger === trigger && (item.name === wanted || item.aliases.includes(wanted)),
        ) ?? null
      );
    },
    [items, provider],
  );

  const close = () => setDismissedAt(value);

  const pick = (item: ICommandItem, via: TPickVia) => {
    if (!listQuery) return;
    const completed = completeCommand(value, listQuery, item);
    const onlyComplete =
      via === 'tab' || (via === 'pointer' && coarse) || item.trigger === '$' || needsArgument(item);
    if (onlyComplete) {
      setValue(completed);
      return;
    }
    // The list stays closed while the command runs and after an error that keeps the text in the field.
    setDismissedAt(`/${item.name}`);
    onRun(item, `/${item.name}`);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>): boolean => {
    if (!open || event.nativeEvent.isComposing) return false;
    if (event.key === 'Escape') {
      // Esc closes the list first, also while it is still loading.
      event.preventDefault();
      event.stopPropagation();
      close();
      return true;
    }
    if (event.key === 'Tab' && !event.shiftKey) {
      const current = nav.activeValue ? byId.get(nav.activeValue) : null;
      if (!current) return false;
      event.preventDefault();
      pick(current, 'tab');
      return true;
    }
    if (event.key === 'Enter' && !event.shiftKey && ranked === null && !catalog.error) {
      // The list is still loading: Enter waits for it instead of sending a half-typed name.
      event.preventDefault();
      return true;
    }
    if (event.key === 'Enter' && (event.shiftKey || !ranked?.items.length)) return false;
    const result = nav.handleKeyDown(event);
    if (!result) return event.defaultPrevented;
    if (result.type === LISTBOX_ACTION.dismiss) {
      close();
      return true;
    }
    const chosen = byId.get(result.value);
    if (chosen) pick(chosen, 'enter');
    return true;
  };

  const argumentHint = (() => {
    if (query?.kind !== 'args') return null;
    const item = resolve(query.name);
    return item?.argumentHint ? `/${item.name} ${item.argumentHint}` : null;
  })();

  const openAll = () => {
    setShowHidden(true);
    setDismissedAt(null);
    setValue('/');
  };

  return {
    listId,
    open,
    query,
    ranked,
    options,
    itemByValue: (id) => byId.get(id) ?? null,
    activeValue: nav.activeValue,
    setActiveValue: nav.setActiveValue,
    activeDescendant: open ? nav.activeDescendant : undefined,
    catalog,
    showHidden,
    setShowHidden,
    argumentHint,
    handleKeyDown,
    pick,
    close,
    openAll,
    resolve,
  };
};

export default useSlashCommands;

export type TMirrorState =
  { kind: 'menu'; command: string; menu: ITuiMenu } | { kind: 'panel'; command: string; screen: string };

interface IConfirmState {
  item: ICommandItem;
  line: string;
}

export interface IUseSlashExecutorOptions {
  provider: TAgentProvider;
  sessionName: string | undefined;
  resolve: (name: string) => ICommandItem | null;
  setValue: (value: string) => void;
  openAll: () => void;
  onRestartSession?: () => void;
  onOpenTerminal: () => void;
}

export interface IUseSlashExecutor {
  running: boolean;
  mirror: TMirrorState | null;
  closeMirror: () => void;
  confirm: IConfirmState | null;
  confirmPending: boolean;
  confirmError: string | null;
  acceptConfirm: () => void;
  cancelConfirm: () => void;
  /** Runs a `/…` line; true when it went through and the field can be cleared, false to keep the text. */
  execute: (line: string) => Promise<boolean>;
}

const LINE_RE = /^\/(\S+)(?:\s+([\s\S]*))?$/;

const appliedMessage = (applied: IApplied): string => {
  if (applied.model) {
    const label = CLAUDE_LAUNCH_MODELS.find((model) => model.id === applied.model)?.label ?? applied.model;
    return t('hooks.useSlashCommands.modelApplied', { label });
  }
  return t('hooks.useSlashCommands.effortApplied', {
    effort: effortLabel(applied.effort ?? ''),
  });
};

const TERMINAL_HINT_ERRORS: readonly TRunError[] = ['menu-open', 'input-not-empty'];

/**
 * For a running conversation: web analogues first (model/effort chips, restart, help, `$` skills, confirmation),
 * then POST /api/platform/commands/run and its outcome: a pending timeline card, a mirrored menu or panel,
 * or an error toast with the text left in the field.
 */
export const useSlashExecutor = ({
  provider,
  sessionName,
  resolve,
  setValue,
  openAll,
  onRestartSession,
  onOpenTerminal,
}: IUseSlashExecutorOptions): IUseSlashExecutor => {
  const [running, setRunning] = useState(false);
  const [mirror, setMirror] = useState<TMirrorState | null>(null);
  const [confirm, setConfirm] = useState<IConfirmState | null>(null);
  const [confirmPending, setConfirmPending] = useState(false);
  const [confirmError, setConfirmError] = useState<string | null>(null);

  const terminalAction = useMemo(
    () => ({ label: t('hooks.useSlashCommands.openTerminal'), onClick: onOpenTerminal }),
    [onOpenTerminal],
  );

  const run = useCallback(
    async (
      line: string,
      item: ICommandItem | null,
    ): Promise<{ ok: boolean; message?: string; notified?: boolean }> => {
      if (!sessionName) return { ok: false, message: t('hooks.useSlashCommands.notStarted') };
      let response: Response;
      try {
        response = await appFetch('/api/platform/commands/run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session: sessionName, provider, line }),
        });
      } catch {
        return { ok: false, message: t('hooks.useSlashCommands.machineNoAnswer') };
      }
      const body = (await response.json().catch(() => null)) as
        (TRunBody & { error?: TRunError; message?: string }) | null;
      if (!response.ok || !body || !('outcome' in body)) {
        const message =
          body?.message ??
          body?.error ??
          t('hooks.useSlashCommands.failedStatus', { status: response.status });
        // Errors about the terminal's own state come with a way to go and fix it there.
        if (body?.error && TERMINAL_HINT_ERRORS.includes(body.error)) {
          toast.error(message, { action: terminalAction });
          return { ok: false, message, notified: true };
        }
        return { ok: false, message };
      }
      const match = line.match(LINE_RE);
      const detail: IPendingSlashCommandDetail = {
        sessionName,
        name: match?.[1] ?? '',
        args: match?.[2] ?? '',
      };
      const terminalOnly = item?.execution === 'terminal';
      if (terminalOnly) {
        onOpenTerminal();
        toast.info(t('hooks.useSlashCommands.openedInTerminal', { command: body.command }));
        return { ok: true };
      }
      if (body.outcome === 'menu') setMirror({ kind: 'menu', command: body.command, menu: body.menu });
      else if (body.outcome === 'panel')
        setMirror({ kind: 'panel', command: body.command, screen: body.screen });
      else {
        window.dispatchEvent(
          new CustomEvent<IPendingSlashCommandDetail>(PENDING_SLASH_COMMAND_EVENT, { detail }),
        );
        if (body.outcome === 'confirmed') toast.success(appliedMessage(body.applied));
        else if (body.expect === 'screen')
          toast.info(t('hooks.useSlashCommands.resultInTerminal', { command: body.command }), {
            action: terminalAction,
          });
        if (/^\/reload-(plugins|skills)$/i.test(body.command)) refreshCommandCatalogs(provider);
      }
      return { ok: true };
    },
    [onOpenTerminal, provider, sessionName, terminalAction],
  );

  const execute = useCallback(
    async (line: string): Promise<boolean> => {
      const text = line.trim();
      const match = text.match(LINE_RE);
      if (!match || running) return false;
      const [, name, args = ''] = match;
      const item = resolve(name);
      const execution = item?.execution ?? 'send';
      if (execution === 'blocked') {
        toast.error(
          item?.note
            ? t('hooks.useSlashCommands.blockedWithNote', { name: item?.name ?? name, note: item.note })
            : t('hooks.useSlashCommands.blocked', { name: item?.name ?? name }),
        );
        return false;
      }
      if ((execution === 'model' || execution === 'effort') && !args.trim()) {
        const detail: IOpenLaunchPickerDetail = { sessionName, section: execution };
        window.dispatchEvent(new CustomEvent(OPEN_LAUNCH_PICKER_EVENT, { detail }));
        return true;
      }
      if (execution === 'restart') {
        if (!onRestartSession) return false;
        onRestartSession();
        return true;
      }
      if (execution === 'help') {
        openAll();
        return false;
      }
      if (execution === 'skills') {
        setValue('$');
        return false;
      }
      if (execution === 'confirm' && item) {
        setConfirmError(null);
        setConfirm({ item, line: text });
        return false;
      }
      setRunning(true);
      try {
        const result = await run(text, item);
        if (!result.ok && result.message && !result.notified) toast.error(result.message);
        return result.ok;
      } finally {
        setRunning(false);
      }
    },
    [onRestartSession, openAll, resolve, run, running, sessionName, setValue],
  );

  const acceptConfirm = useCallback(() => {
    if (!confirm || confirmPending) return;
    setConfirmPending(true);
    setConfirmError(null);
    void run(confirm.line, confirm.item)
      .then((result) => {
        if (!result.ok) {
          setConfirmError(result.message ?? t('hooks.useSlashCommands.failed'));
          return;
        }
        setConfirm(null);
        setValue('');
      })
      .finally(() => setConfirmPending(false));
  }, [confirm, confirmPending, run, setValue]);

  return {
    running,
    mirror,
    closeMirror: () => setMirror(null),
    confirm,
    confirmPending,
    confirmError,
    acceptConfirm,
    cancelConfirm: () => {
      if (!confirmPending) setConfirm(null);
    },
    execute,
  };
};
