// «GPT в Claude Code» at the foot of the Codex card: one switch that adds the Codex models to the model picker of Claude
// Code chats. It runs on the Codex sign-in by subscription through a small proxy the machine starts, so it is offered only
// while Codex is signed in that way, and it never changes the Codex chats themselves. The switch is pessimistic: it moves
// when the machine says the proxy is up (or down), and a refusal is said under it with a way to try again.
import { useEffect, useRef, useState } from 'react';
import type { IPresetState } from '@/next/features/providers/model';
import { SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';

interface IGptSwitchProps {
  /** The runtime's `gpt` profile; undefined from a runtime that has none (the block is then not drawn). */
  preset: IPresetState | undefined;
  /** Codex is signed in by subscription on the machine: the only sign-in the profile can run on. */
  ready: boolean;
  /** Turns the profile on or off; resolves null when the machine did it, else the reason. */
  onSwitch: (on: boolean) => Promise<string | null>;
}

const GptSwitch = ({ preset, ready, onSwitch }: IGptSwitchProps) => {
  const [busy, setBusy] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);
  /** What the last press asked for: «Повторить» asks it again. */
  const [tried, setTried] = useState(true);
  const [more, setMore] = useState(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  // The failure line stays on screen while its row closes.
  const [kept, setKept] = useState('');
  if (error !== null && error !== kept) setKept(error);

  if (!preset?.switch) return null;
  const on = preset.switch.on && ready;
  const models = preset.models.map((m) => m.label).join(', ');

  const flip = async (next: boolean) => {
    setError(null);
    setTried(next);
    setBusy(next);
    const refused = await onSwitch(next);
    if (!alive.current) return;
    setBusy(null);
    setError(refused);
  };

  return (
    <div className="hx-gpt">
      <div className="hx-gpt__head">
        <div className="hx-gpt__text">
          <div className="hx-gpt__name">{t('next.providers.gpt.title')}</div>
          <p className="hx-preset__where">
            {ready ? t('next.providers.gpt.hint') : t('next.providers.gpt.needLogin')}
            {ready && (
              <>
                {' · '}
                <button
                  type="button"
                  className="hx-gpt__more"
                  aria-expanded={more}
                  onClick={() => setMore(!more)}
                >
                  {t(more ? 'next.providers.gpt.less' : 'next.providers.gpt.more')}
                </button>
              </>
            )}
          </p>
        </div>
        <Switch
          label={t('next.providers.gpt.title')}
          checked={busy ?? on}
          disabled={!ready || busy !== null}
          onChange={(next) => void flip(next)}
        />
      </div>
      <div className="hx-reveal" data-open={ready && more}>
        <div className="hx-reveal__in" inert={!(ready && more) || undefined}>
          <p className="hx-gpt__note">{t('next.providers.gpt.how')}</p>
        </div>
      </div>
      <div className="hx-reveal" data-open={busy === true}>
        <div className="hx-reveal__in">
          <div className="hx-preset__saved hx-gpt__state">
            <Icon name="loader-circle" size="sm" className="hx-spin-ic" />
            <span>{t('next.providers.gpt.starting')}</span>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={on && busy === null}>
        <div className="hx-reveal__in" inert={!on || undefined}>
          <div className="hx-preset__saved hx-gpt__state">
            <Icon name="check" size="sm" />
            <span>{t('next.providers.gpt.on', { models })}</span>
          </div>
          <p className="hx-preset__where hx-gpt__sub">{t('next.providers.gpt.pick')}</p>
        </div>
      </div>
      <div className="hx-reveal" data-open={error !== null}>
        <div className="hx-reveal__in">
          <div className="hx-preset__test hx-gpt__state" role="status">
            <Icon name="triangle-alert" size="sm" />
            <span>{kept}</span>
          </div>
          <div className="hx-preset__row hx-gpt__retry">
            <SetButton tone="ghost" onClick={() => void flip(tried)}>
              {t('next.providers.gpt.retry')}
            </SetButton>
          </div>
        </div>
      </div>
    </div>
  );
};

export default GptSwitch;
