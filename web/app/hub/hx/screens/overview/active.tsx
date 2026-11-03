// «Кто сегодня в работе»: everyone of the scope with what they did today — at work right now, the chats of the day and
// the merge requests they ended with, the day's money and when each last worked through a machine. Opens from the
// «Активны сегодня» tile; `?active=1` in the address keeps it open, so Back closes it.
import { useNow } from '../../../lib/hooks';
import { navigate } from '../../../lib/router';
import type { ActivePerson } from '../../../org/api/overview';
import { useOrg } from '../../../org/context';
import { Drawer, initial } from '../../ui';
import { useUrl } from '../teams/url';
import { relativeWidths } from './calc';
import { css } from './parts';
import { ago, plural, rub, tokens } from './fmt';

export const ACTIVE_CARDS = ['active'] as const;

const chatsText = (n: number) => `${n} ${plural(n, 'чат', 'чата', 'чатов')}`;
const mrText = (n: number) => `${n} MR`;

/** «работает сейчас», «был(а) в работе 3 ч назад», «ещё не работал(а) через машину». */
const whenText = (p: ActivePerson, now: number): string => {
  if (p.working) return 'работает сейчас';
  if (!p.lastAt) return 'ещё не работал(а) через машину';
  return `последний чат ${ago(p.lastAt, now)}`;
};

const subOf = (p: ActivePerson, now: number): string =>
  [
    whenText(p, now),
    p.chats > 0 ? `сегодня ${chatsText(p.chats)}` : '',
    p.closed > 0 ? `закрыто ${mrText(p.closed)}` : '',
    p.tokens > 0 ? `${tokens(p.tokens)} токенов` : '',
  ]
    .filter(Boolean)
    .join(' · ');

const todayOf = (p: ActivePerson): boolean => p.working || p.chats > 0;

const Row = ({ p, w, now, base }: { p: ActivePerson; w: string; now: number; base: string }) => (
  <li className={`dash-bar${todayOf(p) ? '' : ' dash-bar--muted'}`} data-action="true">
    <button
      type="button"
      className="dash-bar__hit"
      onClick={() => navigate(`${base}/teams?p=${encodeURIComponent(p.userId)}`)}
    >
      <span className="dash-bar__fill" aria-hidden="true" style={css({ '--dash-w': w })} />
      <span className="dash-bar__label ac-who">
        <span className="ac-ava" data-on={p.working ? '1' : undefined} aria-hidden="true">
          {initial(p.name)}
        </span>
        <span className="ac-who__t">
          {p.name}
          <small>{subOf(p, now)}</small>
        </span>
      </span>
      <span className="dash-bar__value">{p.listRub > 0 ? rub(p.listRub) : '—'}</span>
    </button>
  </li>
);

const Group = ({
  title,
  people,
  now,
  base,
}: {
  title: string;
  people: ActivePerson[];
  now: number;
  base: string;
}) => {
  if (people.length === 0) return null;
  const widths = relativeWidths(people.map((p) => p.listRub || p.tokens / 1e6));
  return (
    <div className="md-sec">
      <div className="md-sec__t">{title}</div>
      <ul className="dash-bars sp-rows">
        {people.map((p, i) => (
          <Row key={p.userId} p={p} w={widths[i]!} now={now} base={base} />
        ))}
      </ul>
    </div>
  );
};

export const ActiveDrawer = ({ who, total }: { who: ActivePerson[]; total: number }) => {
  const url = useUrl(ACTIVE_CARDS);
  const { base } = useOrg();
  const open = url.get('active') !== null;
  const now = useNow(60_000, open);
  // At work right now first, then by the day's money: the drawer answers «who is busy and on what».
  const today = who
    .filter(todayOf)
    .sort((a, b) => Number(b.working) - Number(a.working) || b.listRub - a.listRub || b.tokens - a.tokens);
  const rest = who.filter((p) => !todayOf(p));
  const working = who.filter((p) => p.working).length;
  const chats = today.reduce((n, p) => n + p.chats, 0);
  const closed = today.reduce((n, p) => n + p.closed, 0);
  return (
    <Drawer
      open={open}
      onClose={url.close}
      className="md sp"
      label="Активны сегодня"
      title="Кто сегодня в работе"
      sub={`${today.length} из ${total || who.length}`}
    >
      {open && (
        <>
          <div className="md-res ps-stats">
            <div>
              <b>{working}</b>
              <small>работают прямо сейчас</small>
            </div>
            <div>
              <b>{chats}</b>
              <small>{`${plural(chats, 'чат', 'чата', 'чатов')} сегодня`}</small>
            </div>
            <div>
              <b>{closed}</b>
              <small>закрыто с MR</small>
            </div>
          </div>
          <Group title="Сегодня в работе" people={today} now={now} base={base} />
          <Group title="Сегодня не работали" people={rest} now={now} base={base} />
          {who.length === 0 && <p className="md-note md-sec">В организации пока никого.</p>}
          <p className="md-note md-sec">
            Активен — у человека был чат на одной из машин организации сегодня. Сумма — работа дня по прайсу
            API: токены × цена модели.
          </p>
        </>
      )}
    </Drawer>
  );
};

export const useOpenActive = () => {
  const url = useUrl(ACTIVE_CARDS);
  return () => url.open({ active: '1' });
};
