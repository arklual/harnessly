// The agent's analysis under a comment or a discussion (board «Ревью кода — дифф»): a dashed, tinted box «видно только
// вам» with a shimmer while the agent writes, its weighing of each point and the conclusion, and a ready answer from the
// person — another variant, edited by the pencil, «Применить ответ» (⌘/Ctrl+Enter) sends it to the discussion in the
// person's name. Without the person's own GitLab (GitHub) connection the answer asks to connect it.
import { useEffect, useState } from 'react';
import {
  analysisQuestion,
  parseAnalysis,
  type IAnalysis,
  type IAnalysisNote,
} from '@/next/features/review/app/thread-analysis';
import type { TForge } from '@/next/features/review/app/mr-review';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

type TState = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; analysis: IAnalysis };

interface IAnalysisBoxProps {
  notes: readonly IAnalysisNote[];
  /** The comment weighed; null for the whole discussion. */
  index: number | null;
  where: string;
  /** Where the answer goes: GitLab or GitHub. */
  forge: TForge;
  analyze: (question: string, signal: AbortSignal) => Promise<string>;
  /** Sends the answer to the discussion; false when it did not go. Absent without the person's own connection. */
  onApply?: (text: string) => Promise<boolean>;
  onConnect: () => void;
  onClose: () => void;
  closing: boolean;
}

export const AnalysisBox = ({
  notes,
  index,
  where,
  forge,
  analyze,
  onApply,
  onConnect,
  onClose,
  closing,
}: IAnalysisBoxProps) => {
  const [state, setState] = useState<TState>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [variant, setVariant] = useState(0);
  const [edit, setEdit] = useState<string | null>(null);
  /** The answer as the person last changed it; null: the agent's variant. */
  const [custom, setCustom] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const one = index === null ? null : (notes[index] ?? null);

  useEffect(() => {
    const abort = new AbortController();
    const question = analysisQuestion(notes, index, where, {
      askOne: t('next.reviewApp.ai.askOne'),
      askAll: t('next.reviewApp.ai.askAll'),
      formatOne: t('next.reviewApp.ai.formatOne'),
      formatAll: t('next.reviewApp.ai.formatAll'),
    });
    analyze(question, abort.signal).then(
      (answer) => {
        if (abort.signal.aborted) return;
        const analysis = parseAnalysis(answer, one);
        setState(analysis ? { k: 'ready', analysis } : { k: 'failed', text: t('next.reviewApp.ai.unread') });
      },
      (e: unknown) => {
        if (!abort.signal.aborted)
          setState({ k: 'failed', text: e instanceof Error ? e.message : String(e) });
      },
    );
    return () => abort.abort();
    // The question is asked once per open box (and per «Повторить»); the notes it quotes are those it opened over.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attempt]);

  const replies = state.k === 'ready' ? state.analysis.replies : [];
  const reply = edit ?? custom ?? replies[variant % Math.max(1, replies.length)] ?? '';
  const apply = async () => {
    const text = reply.trim();
    if (!text || !onApply || busy) return;
    setBusy(true);
    const ok = await onApply(text);
    setBusy(false);
    if (ok) onClose();
  };

  return (
    <div
      className="hx-rvai"
      role="region"
      aria-label={t('next.reviewApp.ai.region')}
      data-state={closing ? 'closing' : 'open'}
    >
      <div className="hx-rvai__head">
        <span className="hx-rvai__title">
          <Icon name="sparkles" size="sm" />
          {one ? t('next.reviewApp.ai.titleOne') : t('next.reviewApp.ai.titleAll')}
        </span>
        <span className="hx-rvai__only" title={t('next.reviewApp.ai.onlyHint', { forge })}>
          <Icon name="eye-off" size="sm" />
          {t('next.reviewApp.ai.only')}
        </span>
        <RoundButton
          icon="x"
          label={t('next.reviewApp.ai.close')}
          size="sm"
          variant="ghost"
          onClick={onClose}
        />
      </div>
      {state.k === 'loading' && (
        <div className="hx-rvai__sk" role="status" aria-label={t('next.reviewApp.ai.writing')}>
          <span className="hx-sk hx-rvai__line" data-w="90" />
          <span className="hx-sk hx-rvai__line" data-w="75" />
          <span className="hx-sk hx-rvai__line" data-w="45" />
        </div>
      )}
      {state.k === 'failed' && (
        <div className="hx-rvai__failed">
          <p className="hx-rvai__p">{state.text}</p>
          <Button
            size="sm"
            variant="ghost"
            icon="rotate-cw"
            onClick={() => {
              setState({ k: 'loading' });
              setAttempt((n) => n + 1);
            }}
          >
            {t('next.reviewApp.ai.again')}
          </Button>
        </div>
      )}
      {state.k === 'ready' && (
        <>
          {state.analysis.points.map((p, i) => (
            // The points are the agent's, in its order; they have no ids of their own.
            <p key={i} className="hx-rvai__p">
              {!one && <b>{p.who}. </b>}
              {p.why}
            </p>
          ))}
          {state.analysis.sum && (
            <p className="hx-rvai__p hx-rvai__sum">
              <b>{t('next.reviewApp.ai.sum')} </b>
              {state.analysis.sum}
            </p>
          )}
          {replies.length > 0 && (
            <div className="hx-rvai__reply">
              <div className="hx-rvai__rl">
                <span>{t('next.reviewApp.ai.fromYou')}</span>
                {edit === null && replies.length > 1 && (
                  <RoundButton
                    icon="rotate-cw"
                    label={t('next.reviewApp.ai.another')}
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setCustom(null);
                      setVariant((v) => v + 1);
                    }}
                  />
                )}
                {edit === null && (
                  <RoundButton
                    icon="pencil"
                    label={t('next.reviewApp.ai.edit')}
                    size="sm"
                    variant="ghost"
                    onClick={() => setEdit(reply)}
                  />
                )}
              </div>
              {edit === null ? (
                <p className="hx-rvai__text">{reply}</p>
              ) : (
                <textarea
                  className="hx-rvnote__field"
                  value={edit}
                  autoFocus
                  rows={3}
                  aria-label={t('next.reviewApp.ai.fromYou')}
                  onFocus={(event) => {
                    const end = event.currentTarget.value.length;
                    event.currentTarget.setSelectionRange(end, end);
                  }}
                  onChange={(event) => setEdit(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                      event.preventDefault();
                      void apply();
                    } else if (event.key === 'Escape') {
                      event.preventDefault();
                      setEdit(null);
                    }
                  }}
                />
              )}
            </div>
          )}
          {replies.length > 0 && (
            <div className="hx-rvnote__actions">
              <span className="hx-rvnote__hint">
                {onApply
                  ? t('next.reviewApp.ai.goesAs', { forge })
                  : t('next.reviewApp.ai.needOwn', { forge })}
              </span>
              {edit !== null && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setCustom(edit);
                    setEdit(null);
                  }}
                >
                  {t('next.reviewApp.ai.done')}
                </Button>
              )}
              {onApply ? (
                <Button
                  size="sm"
                  variant="primary"
                  icon="send"
                  busy={busy}
                  disabled={!reply.trim()}
                  onClick={() => void apply()}
                >
                  {t('next.reviewApp.ai.apply')}
                </Button>
              ) : (
                <Button size="sm" icon="plug" onClick={onConnect}>
                  {t('next.reviewApp.mr.connectOwn', { forge })}
                </Button>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
};
