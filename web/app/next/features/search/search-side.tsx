// The side of the search (⌘K): what the selected row is and what can be done with it. The name is the largest thing
// here; under it come the last words of a chat, a cover, facts and lists of what belongs to it, then the actions.
import { useState, type KeyboardEvent } from 'react';
import type { IDetailItem, IRowDetail } from '@/next/features/search/detail';
import { partsOf } from '@/next/features/search/highlight';
import type { ISearchRow } from '@/next/features/search/search-dialog';
import { SUBJECT } from '@/next/features/search/subject';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { Skeleton } from '@/next/ui/skeleton';

export const SIDE_ACT = {
  widget: 'widget',
  open: 'open',
  aside: 'aside',
  chat: 'chat',
  copy: 'copy',
} as const;
export type TSideAct = (typeof SIDE_ACT)[keyof typeof SIDE_ACT];

export interface ISideAction {
  id: TSideAct;
  icon: string;
  label: string;
  /** The key that does it; '' for none. */
  key: string;
}

const SENT_MS = 1800;

interface ISearchSideProps {
  row: ISearchRow;
  /** What the row is, in a word. */
  kind: string;
  /** The words lit in the texts. */
  lit: string;
  /** null while the search has no source to ask. */
  detail: IRowDetail | null;
  actions: readonly ISideAction[];
  copied: boolean;
  /** A chat's stop or wake is on its way. */
  busy: boolean;
  onAct: (id: TSideAct) => void;
  onItem: (item: IDetailItem) => void;
  onSend?: (chatId: string, text: string) => Promise<boolean>;
}

const Lit = ({ text, query }: { text: string; query: string }) => (
  <>
    {partsOf(text, query).map((part, i) => (
      <span key={i} data-hit={part.hit || undefined}>
        {part.text}
      </span>
    ))}
  </>
);

const SearchSide = ({
  row,
  kind,
  lit,
  detail,
  actions,
  copied,
  busy,
  onAct,
  onItem,
  onSend,
}: ISearchSideProps) => {
  const [line, setLine] = useState('');
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const shown = detail?.detail ?? null;
  const loading = detail?.loading ?? false;
  const chatId = row.subject?.k === SUBJECT.chat ? row.subject.id : null;
  const fallback = row.preview?.text || row.snippet || '';
  const text = shown?.text ?? (shown && shown.quotes.length > 0 ? '' : fallback);

  const send = () => {
    const words = line.trim();
    if (!onSend || chatId === null || words === '' || sending) return;
    setSending(true);
    void onSend(chatId, words).then((taken) => {
      setSending(false);
      if (!taken) return;
      setLine('');
      setSent(true);
      setTimeout(() => setSent(false), SENT_MS);
    });
  };

  const onLineKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter' || event.nativeEvent.isComposing) return;
    event.preventDefault();
    send();
  };

  return (
    <aside className="hx-srch__side">
      <div className="hx-srch__side-body">
        <div>
          <div className="hx-srch__side-kind">{kind}</div>
          <h3 className="hx-srch__side-title">{row.title}</h3>
        </div>
        {shown?.cover && (
          <img className="hx-srch__cover" src={shown.cover.src} alt={shown.cover.alt} loading="lazy" />
        )}
        {shown?.quotes.map((quote, i) => (
          <div key={i} className="hx-srch__quote">
            <div className="hx-srch__quote-who">
              <span>{quote.who}</span>
              {quote.when && <span>{quote.when}</span>}
            </div>
            <div className="hx-srch__quote-text">
              <Lit text={quote.text} query={lit} />
            </div>
          </div>
        ))}
        {text !== '' && (
          <div className="hx-srch__side-text">
            <Lit text={text} query={lit} />
          </div>
        )}
        {loading && (
          <div className="hx-srch__side-sk" role="status" aria-label={t('next.search.detailLoading')}>
            <Skeleton className="hx-srch__sk-line" />
            <Skeleton className="hx-srch__sk-line" />
            <Skeleton className="hx-srch__sk-line hx-srch__sk-line--short" />
          </div>
        )}
        {shown && shown.facts.length > 0 && (
          <dl className="hx-srch__facts">
            {shown.facts.map((fact) => (
              <div key={fact.label} className="hx-srch__fact">
                <dt>{fact.label}</dt>
                <dd title={fact.title}>{fact.value}</dd>
              </div>
            ))}
          </dl>
        )}
        {shown?.lists.map((list) => (
          <div key={list.label} className="hx-srch__sublist">
            <div className="hx-srch__side-kind">{list.label}</div>
            {list.items.map((item) => (
              <button
                key={item.id}
                type="button"
                className="hx-srch__subitem"
                disabled={!item.go}
                title={item.title}
                onClick={() => onItem(item)}
              >
                <Icon name={item.icon} size="sm" />
                <span>{item.title}</span>
                {item.meta && <small>{item.meta}</small>}
              </button>
            ))}
          </div>
        ))}
      </div>
      <div className="hx-srch__side-foot">
        {shown?.reply && onSend && chatId !== null && (
          <label className="hx-srch__reply">
            <input
              type="text"
              value={line}
              placeholder={sent ? t('next.search.replySent') : t('next.search.replyPlaceholder')}
              aria-label={t('next.search.replyPlaceholder')}
              autoComplete="off"
              disabled={sending}
              onChange={(event) => setLine(event.target.value)}
              onKeyDown={onLineKey}
            />
            <button
              type="button"
              className="hx-srch__reply-go"
              aria-label={t('next.search.replySend')}
              disabled={sending || line.trim() === ''}
              onClick={send}
            >
              <Icon name="arrow-up" size="sm" />
            </button>
          </label>
        )}
        <div className="hx-srch__acts">
          {actions.map((action) => (
            <button
              key={action.id}
              type="button"
              className="hx-srch__act"
              disabled={action.id === SIDE_ACT.chat && busy}
              onClick={() => onAct(action.id)}
            >
              <Icon name={action.icon} size="sm" />
              <span>{action.id === SIDE_ACT.copy && copied ? t('next.search.copied') : action.label}</span>
              {action.key !== '' && <kbd>{action.key}</kbd>}
            </button>
          ))}
        </div>
      </div>
    </aside>
  );
};

export default SearchSide;
