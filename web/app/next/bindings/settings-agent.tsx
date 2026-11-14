// The agent of the Settings window: the window's one field at the bottom of the page and,
// over it, the answer. A question goes to the window's agent (POST ui-agent): a quick model answers at once, with no chat
// and no terminal. It may take the window to the place it names (`go`, done at once), or offer a change the person
// confirms (`confirm`: «Поменять» sets it as the person's own, with the journal's way back), and says a few words.
import { useCallback, useEffect, useRef, useState } from 'react';
import { type ISettingChangesHandle } from '@/next/bindings/settings-changes';
import { useDraftField } from '@/next/bindings/use-draft';
import { useVoice } from '@/next/bindings/use-voice';
import DockBar from '@/next/features/composer/dock-bar';
import FieldFold from '@/next/features/composer/field-fold';
import ScreenField from '@/next/features/composer/one-field';
import AgentIsland, {
  ISLAND,
  type IIslandAction,
  type IIslandChange,
  type IIslandConfirm,
  type TIslandPhase,
} from '@/next/features/settings/agent-island';
import { harnesslyMap } from '@/next/features/settings/agent-map';
import { putSetting } from '@/next/features/settings/changes-api';
import { loadSmart, type ISmartView } from '@/next/features/settings/smart-api';
import { isRefused } from '@/next/features/settings/platform-answer';
import {
  askUIAgent,
  settingPlaces,
  UI_EVENT,
  UI_NO_MODEL,
  type TUIEvent,
} from '@/next/features/settings/ui-agent-api';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, parseRoute, ROUTE } from '@/next/routes/routes';
import { clearDraft, type ScreenDraftKey } from '@/next/stores/drafts';
import { limitsOf } from '@/next/stores/machines';
import { useEnterSends } from '@/next/stores/me';
import { Button } from '@/next/ui/button';

/** How long the card over the field takes to leave (styles/settings.css, .hx-isle__reply[data-state]). */
const CLOSE_MS = 160;

/** The window's field. */
const DRAFT: ScreenDraftKey = 'screen:settings';

/** A change the agent offered: waiting for the person, being made, made, or being taken back. */
interface IOffer {
  key: string;
  value: string;
  title: string;
  was: string;
  now: string;
  state: 'asked' | 'making' | 'made' | 'undoing';
  /** The setting's value before the change, as the journal wrote it: the way back. */
  old: string | null;
}

/** One question and what came of it. */
interface IAnswer {
  phase: TIslandPhase;
  text: string;
  offer: IOffer | null;
  /** The machine has no model for the window's agent. */
  noModel: boolean;
}

const QUIET: IAnswer = { phase: ISLAND.idle, text: '', offer: null, noModel: false };

const go = (path: string) => goTo(parseRoute(path));

/** The model the window's assistant answers on, as Smart features names it; null while the machine picks a quick one. */
const assistantModel = (view: ISmartView): string | null => {
  const choice = view.features.find((feature) => feature.id === 'ui-agent')?.choice;
  if (!choice) return null;
  const provider = view.providers.find((p) => p.id === choice.provider);
  return provider?.models.find((m) => m.id === choice.model)?.label || choice.model || provider?.name || null;
};

const noStop = () => Promise.resolve();

interface ISettingsAgentProps {
  machine: MachineId;
  changes: ISettingChangesHandle;
}

const SettingsAgent = ({ machine, changes }: ISettingsAgentProps) => {
  const enterSends = useEnterSends();
  const field = useRef<HTMLTextAreaElement>(null);
  const { text: typed, setText, read } = useDraftField(DRAFT, field);
  const {
    notice: voiceNotice,
    recording,
    recognized,
    caret,
    onMic,
    onCancelRecording,
    onFinishRecording,
  } = useVoice(machine, typed, setText, read, DRAFT);
  const [answer, setAnswer] = useState<IAnswer>(QUIET);
  const [closing, setClosing] = useState(false);
  const asking = useRef<AbortController | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      asking.current?.abort();
      clearTimeout(closeTimer.current ?? undefined);
    },
    [],
  );
  // Another machine's window: what was asked of this one is not its answer.
  const [answerOf, setAnswerOf] = useState(machine);
  if (answerOf !== machine) {
    setAnswerOf(machine);
    setAnswer(QUIET);
  }
  useEffect(() => () => asking.current?.abort(), [machine]);

  // The field's model is the assistant's, picked in Smart features: the pill names it and leads there.
  const [model, setModel] = useState<{ machine: MachineId; label: string | null } | null>(null);
  useEffect(() => {
    let live = true;
    void loadSmart(machine).then((res) => {
      if (live && 'ok' in res && !isRefused(res.value))
        setModel({ machine, label: assistantModel(res.value) });
    });
    return () => {
      live = false;
    };
  }, [machine]);
  const modelLabel = model?.machine === machine ? model.label : null;

  const onEvent = useCallback((event: TUIEvent) => {
    switch (event.type) {
      case UI_EVENT.go:
        go(event.place);
        return;
      case UI_EVENT.confirm:
        setAnswer((was) => ({
          ...was,
          offer: {
            key: event.key,
            value: event.value,
            title: event.title,
            was: event.was,
            now: event.label,
            state: 'asked',
            old: null,
          },
        }));
        return;
      case UI_EVENT.delta:
        setAnswer((was) => ({ ...was, phase: ISLAND.answering, text: was.text + event.text }));
        return;
      case UI_EVENT.done:
        setAnswer((was) => ({ ...was, phase: ISLAND.done, text: event.text || was.text }));
        return;
      case UI_EVENT.error:
        setAnswer({
          phase: ISLAND.failed,
          text:
            event.code === UI_NO_MODEL
              ? t('next.settings.agentIsle.noModel')
              : event.message || t('next.settings.agentIsle.failed'),
          offer: null,
          noModel: event.code === UI_NO_MODEL,
        });
        return;
    }
  }, []);

  const onSubmit = useCallback(() => {
    const query = read().trim();
    if (query === '') return;
    asking.current?.abort();
    const ask = new AbortController();
    asking.current = ask;
    clearTimeout(closeTimer.current ?? undefined);
    setClosing(false);
    setAnswer({ ...QUIET, phase: ISLAND.thinking });
    clearDraft(DRAFT);
    void askUIAgent(
      machine,
      { query, page: window.location.pathname, map: harnesslyMap(), places: settingPlaces() },
      ask.signal,
      (event) => {
        if (asking.current === ask) onEvent(event);
      },
    );
  }, [machine, onEvent, read]);

  const onStop = useCallback(() => {
    asking.current?.abort();
    setAnswer((was) => (was.text === '' ? QUIET : { ...was, phase: ISLAND.done }));
    return Promise.resolve();
  }, []);

  const onDismiss = useCallback(() => {
    setClosing(true);
    closeTimer.current = setTimeout(() => {
      setAnswer(QUIET);
      setClosing(false);
    }, CLOSE_MS);
  }, []);

  const { refresh } = changes;
  const apply = useCallback(
    (offer: IOffer) => {
      setAnswer((was) => (was.offer ? { ...was, offer: { ...was.offer, state: 'making' } } : was));
      void putSetting(machine, offer.key, offer.value).then((res) => {
        const change = 'ok' in res && !isRefused(res.value) ? res.value : null;
        setAnswer((was) => {
          if (was.offer?.key !== offer.key) return was;
          if (change === null)
            return {
              ...was,
              phase: ISLAND.failed,
              text: t('next.settings.agentIsle.notChanged'),
              offer: null,
            };
          return { ...was, offer: { ...was.offer, state: 'made', old: change.old } };
        });
        if (change !== null) refresh();
      });
    },
    [machine, refresh],
  );

  const undo = useCallback(
    (offer: IOffer, old: string) => {
      setAnswer((was) => (was.offer ? { ...was, offer: { ...was.offer, state: 'undoing' } } : was));
      void putSetting(machine, offer.key, old).then((res) => {
        const ok = 'ok' in res && !isRefused(res.value);
        setAnswer((was) => {
          if (was.offer?.key !== offer.key) return was;
          if (!ok) return { ...was, offer: { ...was.offer, state: 'made' } };
          return { ...was, text: t('next.settings.agentIsle.undone'), offer: null };
        });
        if (ok) refresh();
      });
    },
    [machine, refresh],
  );

  const offer = answer.offer;
  // The change waits for the person's word only when the agent has said all it had.
  const confirm: IIslandConfirm | null =
    offer && (offer.state === 'asked' || offer.state === 'making') && answer.phase === ISLAND.done
      ? {
          title: offer.title,
          was: offer.was,
          now: offer.now,
          busy: offer.state === 'making',
          onApply: () => apply(offer),
          onReject: () => setAnswer((was) => ({ ...was, offer: null })),
        }
      : null;
  const old = offer?.old ?? null;
  const made: IIslandChange[] =
    offer && old !== null
      ? [
          {
            id: offer.key,
            title: offer.title,
            was: offer.was,
            now: offer.now,
            busy: offer.state === 'undoing',
            onUndo: () => undo(offer, old),
          },
        ]
      : [];
  const action: IIslandAction | null = answer.noModel
    ? {
        label: t('next.settings.agentIsle.pickModel'),
        onClick: () => goTo({ kind: ROUTE.settings, section: 'smart' }),
      }
    : null;

  const busy = answer.phase === ISLAND.thinking || answer.phase === ISLAND.answering;

  return (
    <AgentIsland
      phase={answer.phase}
      text={answer.text}
      changes={made}
      confirm={confirm}
      action={action}
      closing={closing}
      onDismiss={onDismiss}
      onGo={go}
      field={
        <FieldFold
          hint={typed.trim() || t('next.settings.agentIsle.placeholder')}
          line={busy ? t('next.settings.agentIsle.thinking') : null}
          model={modelLabel ?? t('next.settings.agentIsle.quickModel')}
          limit={null}
          working={busy}
          onStop={() => void onStop()}
          onMic={onMic ? () => onMic() : undefined}
          empty={typed === ''}
        >
          <div className="hx-dock" data-screen>
            <DockBar notice={voiceNotice} />
            <ScreenField
              plain
              enterSends={enterSends}
              value={typed}
              onChange={setText}
              onSubmit={onSubmit}
              mode={busy ? 'working' : 'new'}
              placeholder={t('next.settings.agentIsle.placeholder')}
              onStop={busy ? onStop : noStop}
              picker={null}
              tools={
                <Button
                  size="sm"
                  variant="ghost"
                  icon="layers"
                  onClick={() => goTo({ kind: ROUTE.settings, section: 'smart' })}
                >
                  {modelLabel ?? t('next.settings.agentIsle.quickModel')}
                </Button>
              }
              inputRef={field}
              attachments={[]}
              onFiles={nothing}
              onRemoveAttachment={nothing}
              onRetryAttachment={nothing}
              recording={recording}
              recognized={recognized}
              caret={caret}
              onMic={onMic}
              onCancelRecording={onCancelRecording}
              onFinishRecording={onFinishRecording}
              maxText={limitsOf(machine).maxText}
            />
          </div>
        </FieldFold>
      }
    />
  );
};

const nothing = () => undefined;

export default SettingsAgent;
