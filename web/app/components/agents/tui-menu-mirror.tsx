import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, TwKbd } from '@/components/agents/ui';
import usePoller from '@/hooks/use-poller';
import type { TMirrorState } from '@/hooks/use-slash-commands';
import { appFetch } from '@/lib/app-path';
import type { IMirrorScreen, TMirrorKey } from '@/lib/hl-command-run';
import { optionNumber, stripNumberPrefix } from '@/lib/permission-prompt';
import { PROVIDER_TITLES } from '@/lib/hl-launch-options';
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

const POLL_MS = 700;

const PANEL_KEYS = [
  { key: 'Up', label: '↑', name: t('agents.tuiMenuMirror.up') },
  { key: 'Down', label: '↓', name: t('agents.tuiMenuMirror.down') },
  { key: 'Left', label: '←', name: t('agents.tuiMenuMirror.left') },
  { key: 'Right', label: '→', name: t('agents.tuiMenuMirror.right') },
  { key: 'Tab', label: 'Tab', name: t('agents.tuiMenuMirror.nextTab') },
  { key: 'Space', label: t('agents.tuiMenuMirror.space'), name: t('agents.tuiMenuMirror.space') },
  { key: 'Enter', label: 'Enter', name: 'Enter' },
] as const satisfies readonly { key: TMirrorKey; label: string; name: string }[];

type TView =
  | { kind: 'menu'; screen: IMirrorScreen & { menu: NonNullable<IMirrorScreen['menu']> } }
  | { kind: 'panel'; screen: IMirrorScreen };

const initialView = (mirror: TMirrorState): TView =>
  mirror.kind === 'menu'
    ? { kind: 'menu', screen: { menu: mirror.menu, panel: false, busy: false, screen: '' } }
    : { kind: 'panel', screen: { menu: null, panel: true, busy: false, screen: mirror.screen } };

const viewOf = (screen: IMirrorScreen): TView | null => {
  if (screen.menu) return { kind: 'menu', screen: { ...screen, menu: screen.menu } };
  if (screen.panel) return { kind: 'panel', screen };
  return null;
};

interface ITuiMenuMirrorProps {
  mirror: TMirrorState;
  sessionName: string;
  provider: TAgentProvider;
  onClose: () => void;
  onOpenTerminal: () => void;
}

/**
 * A menu or panel the CLI opened in the (hidden) terminal, shown above the composer: numbered options become
 * buttons, a panel becomes a snapshot with its navigation keys. The screen is re-read while it is open; when the
 * TUI closes it (here or in the terminal) the mirror goes away.
 */
const TuiMenuMirror = ({ mirror, sessionName, provider, onClose, onOpenTerminal }: ITuiMenuMirrorProps) => {
  const [view, setView] = useState<TView>(() => initialView(mirror));
  const [sending, setSending] = useState<TMirrorKey | null>(null);
  const [error, setError] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const title = PROVIDER_TITLES[provider];

  const apply = useCallback(
    (screen: IMirrorScreen, afterKey: TMirrorKey | null) => {
      const next = viewOf(screen);
      if (next) {
        setView(next);
        return;
      }
      if (afterKey && afterKey !== 'Escape')
        toast.success(t('agents.tuiMenuMirror.done', { command: mirror.command }));
      onClose();
    },
    [mirror.command, onClose],
  );

  usePoller(
    sending ? null : `${sessionName}:${mirror.command}`,
    async (signal) => {
      const response = await appFetch(
        `/api/platform/commands/screen?session=${encodeURIComponent(sessionName)}`,
        { signal, cache: 'no-store' },
      );
      if (!response.ok) throw new Error(`screen ${response.status}`);
      apply((await response.json()) as IMirrorScreen, null);
    },
    { intervalMs: POLL_MS, maxBackoffMs: 5000 },
  );

  const press = async (key: TMirrorKey) => {
    if (sending) return;
    setSending(key);
    setError(null);
    try {
      const response = await appFetch('/api/platform/commands/screen', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session: sessionName, key }),
      });
      const body = (await response.json().catch(() => null)) as (IMirrorScreen & { error?: string }) | null;
      if (!response.ok || !body) {
        setError(
          body?.error ?? t('agents.tuiMenuMirror.keyFailed', { provider: title, status: response.status }),
        );
        return;
      }
      apply(body, key);
    } catch {
      setError(t('agents.tuiMenuMirror.noAnswer'));
    } finally {
      setSending(null);
    }
  };

  const choose = (index: number) => {
    if (view.kind !== 'menu') return;
    const number = optionNumber(view.screen.menu.options[index] ?? '');
    if (number && Number(number) <= 9) void press(number as TMirrorKey);
  };

  useEffect(() => {
    const root = rootRef.current;
    const target = root?.querySelector<HTMLElement>('[data-focused="true"], [data-mirror-first]');
    target?.focus({ preventScroll: true });
  }, [view.kind]);

  const heading = view.kind === 'menu' ? (view.screen.menu.title ?? mirror.command) : mirror.command;

  return (
    <div
      ref={rootRef}
      className="tw-tui-mirror"
      role="group"
      aria-label={t(
        view.kind === 'menu'
          ? 'agents.tuiMenuMirror.openedMenuLabel'
          : 'agents.tuiMenuMirror.openedPanelLabel',
        {
          provider: title,
          heading,
        },
      )}
      data-kind={view.kind}
      onKeyDown={(event) => {
        if (event.key !== 'Escape') return;
        event.preventDefault();
        event.stopPropagation();
        void press('Escape');
      }}
    >
      <div className="tw-tui-mirror-head">
        <span className="tw-tui-mirror-eyebrow">
          {view.kind === 'menu'
            ? t('agents.tuiMenuMirror.openedMenu', { provider: title })
            : t('agents.tuiMenuMirror.snapshot', { command: mirror.command })}
        </span>
        {view.kind === 'menu' && <span className="tw-tui-mirror-title">{heading}</span>}
      </div>
      {view.kind === 'menu' ? (
        <>
          {view.screen.menu.request && <p className="tw-tui-mirror-request">{view.screen.menu.request}</p>}
          <div className="tw-tui-mirror-options">
            {view.screen.menu.options.map((option, index) => {
              const number = optionNumber(option);
              const focused = index === view.screen.menu.focusedIndex;
              return (
                <TwButton
                  key={option}
                  size={BUTTON_SIZE.sm}
                  variant={focused ? BUTTON_VARIANT.primary : BUTTON_VARIANT.secondary}
                  className="tw-tui-mirror-option"
                  data-focused={focused}
                  data-mirror-first={index === 0 ? '' : undefined}
                  disabled={!number || Number(number) > 9}
                  pending={sending === number}
                  onClick={() => choose(index)}
                >
                  {number && <span className="tw-tui-mirror-number">{number}</span>}
                  <span className="tw-tui-mirror-label">{stripNumberPrefix(option)}</span>
                </TwButton>
              );
            })}
          </div>
        </>
      ) : (
        <>
          <pre className="tw-tui-mirror-screen" aria-live="polite">
            {view.screen.screen}
          </pre>
          <div className="tw-tui-mirror-keys" role="toolbar" aria-label={t('agents.tuiMenuMirror.keys')}>
            {PANEL_KEYS.map(({ key, label, name }, index) => (
              <TwButton
                key={key}
                size={BUTTON_SIZE.sm}
                variant={BUTTON_VARIANT.ghost}
                aria-label={name}
                data-mirror-first={index === 0 ? '' : undefined}
                pending={sending === key}
                onClick={() => void press(key)}
              >
                <TwKbd>{label}</TwKbd>
              </TwButton>
            ))}
          </div>
        </>
      )}
      {error && (
        <p className="tw-tui-mirror-error" role="alert">
          {error}
        </p>
      )}
      <div className="tw-tui-mirror-foot">
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          pending={sending === 'Escape'}
          onClick={() => void press('Escape')}
        >
          {view.kind === 'menu' ? t('agents.tuiMenuMirror.cancel') : t('agents.tuiMenuMirror.close')}{' '}
          <TwKbd>Esc</TwKbd>
        </TwButton>
        <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={onOpenTerminal}>
          {t('agents.tuiMenuMirror.openTerminal')}
        </TwButton>
      </div>
    </div>
  );
};

export default TuiMenuMirror;
