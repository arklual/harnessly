// The two cards under «Требует внимания», as the board's overview draws them: the spend of the teams against their
// limits and the people who spend the most.
import { useNow } from '../../../lib/hooks';
import { navigate } from '../../../lib/router';
import type { ActivePerson, OverviewPeriod, SpendLine } from '../../../org/api/overview';
import { useOrg } from '../../../org/context';
import { useLoad } from '../../../org/hooks';
import { initial } from '../../ui';
import { ago, forPeriod, rub, thousandBare } from './fmt';
import { css } from './parts';

const TEAM_TONES = ['#8968f4', '#25a897', '#24ae56', '#c8830b', '#d9534a', '#3b82f6'];

const meterTone = (pct: number): string => (pct > 85 ? 'bad' : pct > 65 ? 'warn' : 'ok');

const Meter = ({ pct, tone }: { pct: number; tone?: string }) => (
  <span
    className="hd-meter"
    data-t={tone ?? meterTone(pct)}
    style={css({ '--w': `${Math.max(2, Math.min(100, pct)).toFixed(1)}%` })}
  >
    <i />
  </span>
);

const TeamRow = ({ r, max, base }: { r: SpendLine; max: number; base: string }) => {
  const pct = r.limit ? r.limit.pct : (r.rub / Math.max(1, max)) * 100;
  const go = () => navigate(r.key ? `${base}/team?t=${encodeURIComponent(r.key)}` : `${base}/teams`);
  return (
    <tr className="click" tabIndex={0} title={`Команда ${r.label}: люди, лимит, машины`} onClick={go}>
      <td>
        <span className="row">
          <span className="hd-gmark">{initial(r.label)}</span>
          <span className="nm">{r.label}</span>
        </span>
      </td>
      <td style={{ width: '40%' }}>
        <Meter pct={pct} tone={r.limit ? undefined : 'acc'} />
      </td>
      <td className="num">
        {thousandBare(r.rub)}
        {r.limit && <span className="mut2"> / {thousandBare(r.limit.rub)}</span>}
      </td>
    </tr>
  );
};

/** «Расход по командам»: each team's spend for the period, against its limit when it has one. */
export const TeamsSpendCard = ({ period }: { period: OverviewPeriod }) => {
  const { base } = useOrg();
  const { data } = useLoad((api) => api.spendDetail('team', period), [period]);
  const rows = data?.rows ?? [];
  const max = Math.max(1, ...rows.map((r) => r.rub));
  return (
    <div className="t-card viz-card dash-tile hd-card">
      <div className="ch">
        <h3>Расход по командам</h3>
        <div className="hd-r">
          <span className="mut2 sm">{forPeriod(period)} / лимит, тыс ₽</span>
        </div>
      </div>
      {data && rows.length === 0 ? (
        <p className="mut sm">Расхода за период нет.</p>
      ) : (
        <table className="tbl">
          <tbody>
            {rows.map((r) => (
              <TeamRow key={r.key || '-'} r={r} max={max} base={base} />
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};

/** «Кто больше всех тратит»: the six people with the largest spend of the period. */
export const TopSpendersCard = ({ who }: { who: ActivePerson[] }) => {
  const { base } = useOrg();
  const now = useNow();
  const top = [...who]
    .filter((p) => p.rub > 0)
    .sort((a, b) => b.rub - a.rub)
    .slice(0, 6);
  return (
    <div className="t-card viz-card dash-tile hd-card">
      <div className="ch">
        <h3>Кто больше всех тратит</h3>
        <div className="hd-r">
          <button type="button" className="md-link hd-link" onClick={() => navigate(`${base}/teams`)}>
            Все люди
          </button>
        </div>
      </div>
      {top.length === 0 ? (
        <p className="mut sm">За период никто не тратил.</p>
      ) : (
        <table className="tbl">
          <tbody>
            {top.map((p, i) => (
              <tr
                key={p.userId}
                className="click"
                tabIndex={0}
                title={`${p.name}: роль, команда, лимит, машины`}
                onClick={() => navigate(`${base}/teams?p=${encodeURIComponent(p.userId)}`)}
              >
                <td>
                  <div className="row">
                    <span
                      className="hx-avatar hd-av sm"
                      style={css({ '--c': TEAM_TONES[i % TEAM_TONES.length]! })}
                    >
                      {initial(p.name)}
                    </span>
                    <span>{p.name}</span>
                  </div>
                </td>
                <td className="mut sm">
                  {p.chats} {p.chats === 1 ? 'чат' : p.chats > 1 && p.chats < 5 ? 'чата' : 'чатов'}
                </td>
                <td className="mut sm">{p.working ? 'сейчас' : p.lastAt ? ago(p.lastAt, now) : '—'}</td>
                <td className="num">{rub(p.rub)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};
