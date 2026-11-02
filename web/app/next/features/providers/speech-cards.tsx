// The cloud recognition models' keys on the «Модели и ключи» page: the cards follow the vendors' (.hx-acct) and hold the
// same key panel. The audio of a dictation goes to a provider only when the person picked that model in «Голосовой ввод».
import { useState } from 'react';
import KeyPanel from '@/next/features/providers/key-panel';
import type { SpeechKeyId, SpeechKeys } from '@/next/features/providers/speech-keys';
import { t } from '@/next/i18n';
import '@/next/styles/providers.css';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton } from '@/next/ui/skeleton';

export type SpeechCards =
  | { k: 'loading' }
  | { k: 'failed'; text: string; onRetry: () => void }
  | {
      k: 'ready';
      keys: SpeechKeys;
      onSave: (id: SpeechKeyId, key: string) => Promise<string | null>;
      onRemove: (id: SpeechKeyId) => Promise<string | null>;
    };

const PROVIDERS = [
  { id: 'openai', keys: ['openai'] },
  { id: 'yandex', keys: ['yandex', 'yandex-folder'] },
] as const satisfies readonly { id: string; keys: readonly SpeechKeyId[] }[];

type SpeechProvider = (typeof PROVIDERS)[number]['id'];

interface ICardProps {
  provider: (typeof PROVIDERS)[number];
  cards: Extract<SpeechCards, { k: 'ready' }>;
  open: boolean;
  onToggle: (provider: SpeechProvider) => void;
}

const Card = ({ provider, cards, open, onToggle }: ICardProps) => {
  const main = cards.keys[provider.keys[0]];
  const status = main.configured
    ? t('next.providers.on', { tail: main.tail ?? '' })
    : t('next.providers.off');
  return (
    <div className="hx-acct hx-prov" data-open={open} data-on={main.configured}>
      <button
        type="button"
        className="hx-acct__head"
        aria-expanded={open}
        onClick={() => onToggle(provider.id)}
      >
        <span className="hx-acct__logo">
          <Icon name="mic" size="sm" />
        </span>
        <span className="hx-acct__text">
          <span className="hx-acct__name">{t(`next.speech.${provider.id}`)}</span>
          <span className="hx-acct__status">{status}</span>
        </span>
        <Icon name="chevron-down" size="sm" className="hx-acct__chev" />
      </button>
      <div className="hx-acct__body">
        <div className="hx-acct__inner" inert={!open || undefined}>
          <div className="hx-prov__body">
            {provider.keys.map((id) => (
              <KeyPanel
                key={id}
                hint={t(`next.speech.hint.${id}`)}
                label={t(`next.speech.aria.${id}`)}
                saved={cards.keys[id]}
                onSave={(key) => cards.onSave(id, key)}
                onRemove={() => cards.onRemove(id)}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
};

/** The two cards, or why they are not there. */
const SpeechKeyCards = ({ cards }: { cards: SpeechCards }) => {
  const [open, setOpen] = useState<SpeechProvider | null>(null);
  if (cards.k === 'loading') return <Skeleton className="hx-set__sk" />;
  if (cards.k === 'failed')
    return (
      <div className="hx-acct hx-acct--solo">
        <LoadFailed
          size={LOAD_FAILED_SIZE.inline}
          title={t('next.speech.loadFailed')}
          reason={cards.text}
          onRetry={cards.onRetry}
        />
      </div>
    );
  return (
    <>
      {PROVIDERS.map((provider) => (
        <Card
          key={provider.id}
          provider={provider}
          cards={cards}
          open={open === provider.id}
          onToggle={(id) => setOpen((now) => (now === id ? null : id))}
        />
      ))}
    </>
  );
};

export default SpeechKeyCards;
