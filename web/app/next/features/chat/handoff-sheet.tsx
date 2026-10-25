// The preview of what goes to another chat, editable before it goes: a handoff's context (an excerpt of the
// transcript, or a note the agent writes, chosen on a switch) or a fork's result to merge back. The draft is asked
// of the machine once per kind; what the person typed into it stays while they switch back and forth.
import { useEffect, useRef, useState } from 'react';
import Modal from '@/next/features/shell/modal';
import Segmented from '@/next/ui/segmented';
import { Skeleton } from '@/next/ui/skeleton';
import { HANDOFF_TEXT_MAX } from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';
import '@/next/styles/handoff.css';

export const SHEET_MODE = { excerpt: 'excerpt', note: 'note', result: 'result' } as const;
export type TSheetMode = (typeof SHEET_MODE)[keyof typeof SHEET_MODE];

export interface ISheetDraft {
  text: string;
  tokens: number;
  turns: number;
}

export type TDraftOutcome = { ok: true; draft: ISheetDraft } | { ok: false; reason: string };

type TDraftState =
  { k: 'loading' } | { k: 'failed'; reason: string } | { k: 'ready'; draft: ISheetDraft; text: string };

export interface IHandoffSheetProps {
  open: boolean;
  onClose: () => void;
  title: string;
  lead: string;
  /** The kinds of draft to choose from, the first one shown first; one kind shows no switch. */
  modes: readonly TSheetMode[];
  /** Asks the machine for a draft of the kind. */
  onDraft: (mode: TSheetMode) => Promise<TDraftOutcome>;
  /** Sends the approved text; rejects with the reason in words, and the sheet stays open. */
  onSend: (mode: TSheetMode, text: string) => Promise<void>;
  sendLabel: string;
}

const MODE_LABEL: Record<TSheetMode, string> = {
  [SHEET_MODE.excerpt]: 'next.handoff.mode.excerpt',
  [SHEET_MODE.note]: 'next.handoff.mode.note',
  [SHEET_MODE.result]: 'next.handoff.mode.result',
} as const;

const MODE_HINT: Record<TSheetMode, string> = {
  [SHEET_MODE.excerpt]: 'next.handoff.hint.excerpt',
  [SHEET_MODE.note]: 'next.handoff.hint.note',
  [SHEET_MODE.result]: 'next.handoff.hint.result',
} as const;

/** What the runtime estimates too: about four characters a token. */
const tokensOf = (text: string): number => Math.ceil([...text].length / 4);

/** «≈ 3 200 токенов · 6 ходов» of the text as it stands now. */
const sizeOf = (state: Extract<TDraftState, { k: 'ready' }>): string => {
  const tokens = state.text === state.draft.text ? state.draft.tokens : tokensOf(state.text);
  const parts = [t('next.handoff.tokens', { n: tokens.toLocaleString() })];
  if (state.draft.turns > 0) parts.push(plural(state.draft.turns, 'next.handoff.turns'));
  return parts.join(' · ');
};

const SheetBody = ({
  modes,
  onDraft,
  onSend,
  onClose,
  sendLabel,
}: Omit<IHandoffSheetProps, 'open' | 'title' | 'lead'>) => {
  const [mode, setMode] = useState<TSheetMode>(modes[0] ?? SHEET_MODE.excerpt);
  const [drafts, setDrafts] = useState<Partial<Record<TSheetMode, TDraftState>>>({});
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const asked = useRef(new Set<TSheetMode>());

  const ask = (which: TSheetMode) => {
    asked.current.add(which);
    setDrafts((now) => ({ ...now, [which]: { k: 'loading' } }));
    void onDraft(which).then((outcome) =>
      setDrafts((now) => ({
        ...now,
        [which]: outcome.ok
          ? { k: 'ready', draft: outcome.draft, text: outcome.draft.text }
          : { k: 'failed', reason: outcome.reason },
      })),
    );
  };

  // Each kind is asked the first time it is shown; a note takes the agent a while, an excerpt is at once.
  useEffect(() => {
    if (!asked.current.has(mode)) ask(mode);
  });

  const state = drafts[mode] ?? { k: 'loading' };
  const text = state.k === 'ready' ? state.text.trim() : '';
  const tooLong = [...text].length > HANDOFF_TEXT_MAX;

  const send = async () => {
    if (state.k !== 'ready' || text === '' || tooLong) return;
    setError(null);
    setSending(true);
    try {
      await onSend(mode, text);
      onClose();
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.handoff.failed'));
    } finally {
      setSending(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void send();
      }}
    >
      <div className="hx-modal__body hx-hsheet">
        {modes.length > 1 && (
          <Segmented
            label={t('next.handoff.modeLabel')}
            value={mode}
            options={modes.map((m) => ({ value: m, label: t(MODE_LABEL[m]) }))}
            onChange={(next) => {
              setError(null);
              setMode(next);
            }}
            disabled={sending}
          />
        )}
        <p className="hx-hsheet__hint">{t(MODE_HINT[mode])}</p>
        {state.k === 'loading' && (
          <div className="hx-hsheet__wait" role="status">
            <Skeleton className="hx-hsheet__sk" />
            <span>{mode === SHEET_MODE.excerpt ? t('next.handoff.loading') : t('next.handoff.writing')}</span>
          </div>
        )}
        {state.k === 'failed' && (
          <div className="hx-hsheet__fail" role="alert">
            <span>{state.reason}</span>
            <button type="button" className="hx-btn" data-variant="ghost" onClick={() => ask(mode)}>
              {t('next.load.retry')}
            </button>
          </div>
        )}
        {state.k === 'ready' && (
          <>
            <textarea
              className="hx-hsheet__text"
              aria-label={t('next.handoff.textLabel')}
              value={state.text}
              disabled={sending}
              onChange={(event) => {
                const value = event.target.value;
                setDrafts((now) => ({ ...now, [mode]: { ...state, text: value } }));
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                  event.preventDefault();
                  event.currentTarget.form?.requestSubmit();
                }
              }}
            />
            <p className="hx-hsheet__size" data-over={tooLong || undefined}>
              {tooLong ? t('next.handoff.tooLong') : sizeOf(state)}
            </p>
          </>
        )}
        {error && (
          <p className="hx-create__err" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot">
        <span className="hx-spacer" />
        <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
          {t('next.rowMenu.cancel')}
        </button>
        <button
          type="submit"
          className="hx-btn hx-create__ok"
          data-variant="primary"
          data-busy={sending}
          disabled={state.k !== 'ready' || text === '' || tooLong || sending}
        >
          <span>{sendLabel}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </form>
  );
};

/** The sheet itself: a dialog on a wide window, a sheet from below on the phone (the Modal's own), both animated out. */
const HandoffSheet = ({ open, onClose, title, lead, ...body }: IHandoffSheetProps) => (
  <Modal open={open} onClose={onClose} title={title} lead={lead} boxClass="hx-create hx-hsheet-box">
    <SheetBody {...body} onClose={onClose} />
  </Modal>
);

export default HandoffSheet;
