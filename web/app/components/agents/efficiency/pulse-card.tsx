import { useState } from 'react';
import { postEfficiency, useEfficiency } from '@/hooks/use-efficiency';
import type { IEffPulse } from '@/types/efficiency';
import { shortDay } from '@/components/agents/efficiency/format';
import { Card, Segmented } from '@/components/agents/efficiency/primitives';
import { t } from '@/lib/i18n';

type TScore = '1' | '2' | '3' | '4' | '5';
const SCALE: { value: TScore; label: string }[] = ['1', '2', '3', '4', '5'].map((v) => ({
  value: v as TScore,
  label: v,
}));

const QUESTIONS = [
  {
    key: 'speed',
    text: t('efficiency.pulseCard.speedText'),
    low: t('efficiency.pulseCard.speedLow'),
    high: t('efficiency.pulseCard.speedHigh'),
  },
  {
    key: 'quality',
    text: t('efficiency.pulseCard.qualityText'),
    low: t('efficiency.pulseCard.qualityLow'),
    high: t('efficiency.pulseCard.qualityHigh'),
  },
  {
    key: 'friction',
    text: t('efficiency.pulseCard.frictionText'),
    low: t('efficiency.pulseCard.frictionLow'),
    high: t('efficiency.pulseCard.frictionHigh'),
  },
] as const;

type TAnswer = Record<(typeof QUESTIONS)[number]['key'], TScore | null>;

/** H19: three questions a week, seen only by the person who answers; compared with telemetry in aggregate. */
const PulseCard = () => {
  const pulse = useEfficiency<IEffPulse>('/pulse');
  const [answer, setAnswer] = useState<TAnswer>({ speed: null, quality: null, friction: null });
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  if (pulse.status !== 'ready' || !pulse.data) return null;
  const data = pulse.data;
  const current = data.answers.find((a) => a.week === data.week);
  if (data.answered && !editing) {
    return (
      <p className="mb-4 text-[13px] text-(--t-text-secondary)">
        {t('efficiency.pulseCard.saved', {
          week: shortDay(data.week),
          speed: current?.speed ?? '',
          quality: current?.quality ?? '',
          friction: current?.friction ?? '',
        })}{' '}
        <button type="button" className="underline hover:text-(--t-text)" onClick={() => setEditing(true)}>
          {t('efficiency.pulseCard.edit')}
        </button>
      </p>
    );
  }
  const ready = QUESTIONS.every((q) => answer[q.key] !== null);
  const save = async () => {
    setBusy(true);
    const failed = await postEfficiency('/pulse', {
      speed: Number(answer.speed),
      quality: Number(answer.quality),
      friction: Number(answer.friction),
    });
    setBusy(false);
    setMessage(failed ?? '');
    if (!failed) {
      setEditing(false);
      pulse.refresh();
    }
  };
  return (
    <Card
      title={t('efficiency.pulseCard.title')}
      sub={t('efficiency.pulseCard.sub', { week: shortDay(data.week) })}
    >
      <div className="flex flex-col gap-3">
        {QUESTIONS.map((q) => (
          <div key={q.key} className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1.5">
            <span className="min-w-0 flex-1 basis-64 text-[13px]">
              {q.text}{' '}
              <span className="text-(--t-text-muted)">
                · 1 — {q.low}, 5 — {q.high}
              </span>
            </span>
            <Segmented
              label={q.text}
              value={answer[q.key] ?? ('' as TScore)}
              options={SCALE}
              onChange={(v) => setAnswer({ ...answer, [q.key]: v })}
            />
          </div>
        ))}
        <div className="flex items-center gap-3">
          <button
            type="button"
            className="ts-btn disabled:opacity-60"
            disabled={!ready || busy}
            onClick={() => void save()}
          >
            {t('efficiency.pulseCard.save')}
          </button>
          {message && (
            <span className="text-[13px] text-(--t-status-danger)" role="status">
              {message}
            </span>
          )}
        </div>
      </div>
    </Card>
  );
};

export default PulseCard;
