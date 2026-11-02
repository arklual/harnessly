// The sheets of the teams screen: a person (role, team, personal limit), a team (its limit and how much is handed out),
// the tenant's limit and a new team. Every save is pessimistic: the button spins, the table changes after the hub
// answered; a refusal is shown as the hub says it.
import { type ReactNode, useState } from 'react';

import { Button, TextField } from '@kit';

import { orgErrorText } from '../../api/errors';
import type { LimitRule, Needs, Person, ScopeLimit, TeamBlock, TeamsView } from '../../api/teams';
import { useOrg } from '../../context';
import { Seg } from '../../ui/page';
import { Sheet } from '../../ui/sheet';
import { ago, kRub, levelOf, needsText, people, sharePct, stopOf } from './format';
import { LimitSteps } from './steps';

const useSlug = (): string => {
  const { scope } = useOrg();
  return scope.kind === 'org' ? 'all' : scope.tenant.slug;
};

const PERSON_STEPS = [5_000, 10_000, 15_000, 20_000, 30_000, 50_000, 80_000];
const TEAM_STEPS = [50_000, 70_000, 90_000, 110_000, 120_000, 150_000, 200_000, 300_000];
const TENANT_STEPS = [200_000, 300_000, 400_000, 500_000, 800_000];

const withCurrent = (steps: number[], cur: number | null): number[] =>
  cur === null || steps.includes(cur) ? steps : [...steps, cur].sort((a, b) => a - b);

const Field = ({ title, needs, children }: { title: string; needs: Needs | null; children: ReactNode }) => (
  <section className="ht-field" data-locked={needs !== null || undefined}>
    <h3 className="ht-h">{title}</h3>
    {children}
    {needs !== null && <p className="ht-needs">{needsText(needs)}</p>}
  </section>
);

const Stats = ({ items }: { items: { v: string; k: string }[] }) => (
  <div className="ht-stats">
    {items.map((i) => (
      <div key={i.k}>
        <b>{i.v}</b>
        <small>{i.k}</small>
      </div>
    ))}
  </div>
);

const Rules = ({
  rules,
  onChange,
  pending,
}: {
  rules: LimitRule[];
  onChange: (id: string, rub: number) => void;
  pending: boolean;
}) =>
  rules.length === 0 ? null : (
    <section className="ht-field">
      <h3 className="ht-h">Другие лимиты</h3>
      <ul className="ht-rules">
        {rules.map((r) => (
          <li key={r.id}>
            <span>
              <b>
                {r.axis === 'account' ? 'Аккаунт' : 'Модель'} {r.label}
              </b>
              <small>
                потрачено {kRub(r.spentRub)}
                {r.setBy ? ` · поставил: ${r.setBy}` : ''}
              </small>
            </span>
            <span className="ht-rules__amt">
              {r.canEdit ? (
                <LimitInline value={r.amountRub} pending={pending} onSave={(v) => onChange(r.id, v)} />
              ) : (
                <>
                  <b>{kRub(r.amountRub)}</b>
                  {r.needs && <small>{needsText(r.needs)}</small>}
                </>
              )}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );

const LimitInline = ({
  value,
  pending,
  onSave,
}: {
  value: number;
  pending: boolean;
  onSave: (rub: number) => void;
}) => {
  const [text, setText] = useState(String(value / 1000));
  const rub = Math.round(Number(text.replace(',', '.')) * 1000);
  const bad = !Number.isFinite(rub) || rub <= 0;
  return (
    <span className="ht-inline">
      <TextField
        aria-label="Лимит, тысяч рублей"
        value={text}
        inputMode="decimal"
        onChange={(e) => setText(e.target.value)}
      />
      <Button size="xs" loading={pending} disabled={bad || rub === value} onClick={() => onSave(rub)}>
        Сохранить
      </Button>
    </span>
  );
};

type Done = (view: TeamsView) => void;

export const PersonSheet = ({
  p,
  team,
  teams,
  open,
  now,
  onClose,
  onDone,
}: {
  p: Person | null;
  team: TeamBlock | null;
  teams: TeamBlock[];
  open: boolean;
  now: number;
  onClose: () => void;
  onDone: Done;
}) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const scope = useSlug();
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={p?.name ?? ''}
      sub={
        p ? `${team?.name ?? 'Без команды'} · ${p.role === 'lead' ? 'лид команды' : 'участник'}` : undefined
      }
    >
      {p && (
        <PersonBody
          key={p.id}
          p={p}
          teams={teams}
          now={now}
          api={api}
          org={org}
          scope={scope}
          onClose={onClose}
          onDone={onDone}
        />
      )}
    </Sheet>
  );
};

const PersonBody = ({
  p,
  teams,
  now,
  api,
  org,
  scope,
  onClose,
  onDone,
}: {
  p: Person;
  teams: TeamBlock[];
  now: number;
  api: ReturnType<typeof useOrg>['api'];
  org: string;
  scope: string;
  onClose: () => void;
  onDone: Done;
}) => {
  const [role, setRole] = useState(p.role);
  const [teamId, setTeamId] = useState(p.teamId);
  const [limit, setLimit] = useState<number | null>(p.limitRub);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [confirm, setConfirm] = useState(false);
  const steps = withCurrent(PERSON_STEPS.map(stopOf), p.limitRub);
  const changed = role !== p.role || teamId !== p.teamId || limit !== p.limitRub;

  const run = async (fn: () => Promise<TeamsView>) => {
    setPending(true);
    setError(null);
    try {
      const view = await fn();
      onDone(view);
      return true;
    } catch (e) {
      setError(e);
      return false;
    } finally {
      setPending(false);
    }
  };
  const save = async () => {
    const ok = await run(async () => {
      let view: TeamsView | null = null;
      if (role !== p.role) view = await api.setRole(org, scope, p.id, role);
      if (teamId !== p.teamId) view = await api.moveToTeam(org, scope, p.id, teamId);
      if (limit !== p.limitRub) view = await api.setLimit(org, scope, { kind: 'user', id: p.id }, limit);
      if (!view) throw new Error('nothing to save');
      return view;
    });
    if (ok) onClose();
  };
  const remove = async () => {
    const ok = await run(() => api.removeFromTenant(org, scope, p.id));
    if (ok) onClose();
  };
  const eff = p.effective;
  const share = sharePct(p.spentRub, eff.amountRub);
  return (
    <>
      <Stats
        items={[
          { v: kRub(p.spentRub), k: 'потрачено в этом месяце' },
          { v: p.chatsWeek === null ? '—' : String(p.chatsWeek), k: 'чатов за неделю' },
          { v: p.mrsWeek === null ? '—' : String(p.mrsWeek), k: 'MR за неделю' },
        ]}
      />
      {p.invited ? (
        <p className="ht-note">Приглашение не принято{p.lastActiveAt ? '' : ': человек ещё не заходил'}.</p>
      ) : null}
      {p.lastActiveAt && <p className="ht-note">Заходил(а): {ago(p.lastActiveAt, now)}</p>}
      <div className="ht-eff" data-level={levelOf(share)}>
        <b>Действует: {eff.amountRub === null ? 'без лимита' : kRub(eff.amountRub)}</b>
        <span>
          {eff.source === 'personal'
            ? 'личный лимит'
            : eff.source === 'team'
              ? 'лимит команды'
              : 'лимит тенанта'}
          {eff.setBy ? ` · поставил: ${eff.setBy}` : ''}
          {eff.setAt ? `, ${ago(eff.setAt, now)}` : ''}
        </span>
      </div>

      <Field title="Роль" needs={p.can.role ? null : p.self ? null : p.needs.role}>
        <Seg
          label="Роль"
          value={role}
          onChange={setRole}
          options={[
            { value: 'member', label: 'Участник' },
            { value: 'lead', label: 'Лид команды' },
          ]}
        />
        {!p.can.role && p.self && <p className="ht-needs">Себе менять нельзя</p>}
        <p className="ht-help">Запускает чаты на машинах, к которым у него есть доступ</p>
      </Field>

      <Field title="Команда" needs={p.can.team ? null : p.self ? null : p.needs.team}>
        <Seg
          label="Команда"
          value={teamId ?? ''}
          onChange={(v) => setTeamId(v === '' ? null : v)}
          options={[
            ...teams.filter((t) => t.id !== null).map((t) => ({ value: t.id ?? '', label: t.name })),
            { value: '', label: 'Без команды' },
          ]}
        />
      </Field>

      <Field title="Лимит в месяц" needs={p.can.limit ? null : p.self ? null : p.needs.limit}>
        <LimitSteps
          label="Лимит в месяц"
          steps={steps}
          value={limit ?? steps[0] ?? 0}
          current={p.limitRub}
          floorRub={p.spentRub}
          disabled={!p.can.limit || pending}
          onPick={setLimit}
        />
        {!p.can.limit && p.self && (
          <p className="ht-needs">Себе менять нельзя: лимит ставит другой администратор</p>
        )}
        {p.spentRub > 0 && <p className="ht-help">уже потрачено {kRub(p.spentRub)} — меньше не выбрать</p>}
        <p className="ht-help">
          Когда лимит кончится, агенты встанут до 1-го числа — или пока лимит не поднимут. На 80% придёт
          предупреждение.
        </p>
      </Field>

      <Rules
        rules={p.rules}
        pending={pending}
        onChange={(id, rub) => void run(() => api.setRuleAmount(org, scope, id, rub))}
      />

      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <footer className="ht-foot">
        {p.can.remove &&
          (confirm ? (
            <span className="ht-confirm">
              Убрать {p.name} из тенанта? Доступ к его машинам пропадёт.
              <Button size="xs" appearance="danger" loading={pending} onClick={() => void remove()}>
                Убрать
              </Button>
              <Button size="xs" appearance="ghost" disabled={pending} onClick={() => setConfirm(false)}>
                Нет
              </Button>
            </span>
          ) : (
            <Button appearance="ghost" className="ht-danger" onClick={() => setConfirm(true)}>
              Убрать из тенанта
            </Button>
          ))}
        <span className="ht-foot__right">
          <Button appearance="ghost" onClick={onClose} disabled={pending}>
            Отмена
          </Button>
          <Button appearance="primary" loading={pending} disabled={!changed} onClick={() => void save()}>
            Сохранить
          </Button>
        </span>
      </footer>
    </>
  );
};

const HandedOut = ({ pct }: { pct: number | null }) =>
  pct !== null && pct > 100 ? (
    <p className="ht-warn">
      Личные лимиты раздают {pct}% бюджета команды. Это потолки, не доли: действует самый тесный.
    </p>
  ) : pct !== null ? (
    <p className="ht-help">Личные лимиты раздают {pct}% бюджета команды.</p>
  ) : null;

export const TeamSheet = ({
  t,
  open,
  onClose,
  onDone,
}: {
  t: TeamBlock | null;
  open: boolean;
  onClose: () => void;
  onDone: Done;
}) => {
  const { api, shell } = useOrg();
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={t?.name ?? ''}
      sub={t ? `${t.leadName ? `лид ${t.leadName} · ` : ''}${people(t.people.length)}` : undefined}
    >
      {t && (
        <TeamBody
          key={t.id ?? 'none'}
          t={t}
          api={api}
          org={shell.org.slug}
          onClose={onClose}
          onDone={onDone}
        />
      )}
    </Sheet>
  );
};

const TeamBody = ({
  t,
  api,
  org,
  onClose,
  onDone,
}: {
  t: TeamBlock;
  api: ReturnType<typeof useOrg>['api'];
  org: string;
  onClose: () => void;
  onDone: Done;
}) => {
  const slug = useSlug();
  const [limit, setLimit] = useState<number | null>(t.limitRub);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const steps = withCurrent(TEAM_STEPS.map(stopOf), t.limitRub);
  const share = sharePct(t.spentRub, t.limitRub);
  const run = async (fn: () => Promise<TeamsView>) => {
    setPending(true);
    setError(null);
    try {
      onDone(await fn());
      return true;
    } catch (e) {
      setError(e);
      return false;
    } finally {
      setPending(false);
    }
  };
  const save = async () => {
    if (limit === null) return;
    if (await run(() => api.setLimit(org, slug, { kind: 'team', id: t.id ?? '' }, limit))) onClose();
  };
  return (
    <>
      <Stats
        items={[
          { v: kRub(t.spentRub), k: 'потрачено в этом месяце' },
          { v: String(t.people.length), k: 'человек' },
          { v: share === null ? '—' : `${share}%`, k: 'лимита' },
        ]}
      />
      <Field title="Лимит команды в месяц" needs={t.can.limit ? null : t.needs.limit}>
        <LimitSteps
          label="Лимит команды в месяц"
          steps={steps}
          value={limit ?? steps[0] ?? 0}
          current={t.limitRub}
          floorRub={t.spentRub}
          disabled={!t.can.limit || pending}
          onPick={setLimit}
        />
        <p className="ht-help">уже потрачено {kRub(t.spentRub)} — меньше не выбрать</p>
        {t.siblingsLimitRub > 0 && limit !== null && (
          <p className="ht-help">
            Другим командам тенанта отдано {kRub(t.siblingsLimitRub)}; вместе с этой —{' '}
            {kRub(t.siblingsLimitRub + limit)}.
          </p>
        )}
      </Field>
      <HandedOut pct={t.handedOutPct} />
      <Rules
        rules={t.rules}
        pending={pending}
        onChange={(id, rub) => void run(() => api.setRuleAmount(org, slug, id, rub))}
      />
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <footer className="ht-foot">
        <span className="ht-foot__right">
          <Button appearance="ghost" onClick={onClose} disabled={pending}>
            Отмена
          </Button>
          <Button
            appearance="primary"
            loading={pending}
            disabled={!t.can.limit || limit === t.limitRub}
            onClick={() => void save()}
          >
            Сохранить
          </Button>
        </span>
      </footer>
    </>
  );
};

export const ScopeSheet = ({
  s,
  open,
  onClose,
  onDone,
}: {
  s: ScopeLimit;
  open: boolean;
  onClose: () => void;
  onDone: Done;
}) => {
  const { api, shell } = useOrg();
  const slug = useSlug();
  const [limit, setLimit] = useState<number | null>(s.limitRub);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const steps = withCurrent(TENANT_STEPS.map(stopOf), s.limitRub);
  const save = async () => {
    if (limit === null) return;
    setPending(true);
    setError(null);
    try {
      onDone(await api.setLimit(shell.org.slug, slug, { kind: 'tenant', id: slug }, limit));
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  return (
    <Sheet open={open} onClose={onClose} title="Лимит" sub={s.label}>
      <Stats
        items={[
          { v: kRub(s.spentRub), k: 'потрачено в этом месяце' },
          { v: s.limitRub === null ? '—' : `${sharePct(s.spentRub, s.limitRub) ?? 0}%`, k: 'лимита' },
        ]}
      />
      <Field title="Лимит в месяц" needs={s.can.limit ? null : s.needs}>
        <LimitSteps
          label="Лимит в месяц"
          steps={steps}
          value={limit ?? steps[0] ?? 0}
          current={s.limitRub}
          floorRub={s.spentRub}
          disabled={!s.can.limit || pending}
          onPick={setLimit}
        />
        <p className="ht-help">Тенант делится на команды: лимиты команд — потолки внутри этого.</p>
      </Field>
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <footer className="ht-foot">
        <span className="ht-foot__right">
          <Button appearance="ghost" onClick={onClose} disabled={pending}>
            Отмена
          </Button>
          <Button
            appearance="primary"
            loading={pending}
            disabled={!s.can.limit || limit === s.limitRub}
            onClick={() => void save()}
          >
            Сохранить
          </Button>
        </span>
      </footer>
    </Sheet>
  );
};

export const NewTeamSheet = ({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: Done;
}) => {
  const { api, shell } = useOrg();
  const slug = useSlug();
  const [name, setName] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const create = async () => {
    setPending(true);
    setError(null);
    try {
      onDone(await api.createTeam(shell.org.slug, slug, name));
      setName('');
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="Новая команда"
      sub="Людей и лимит можно назначить после создания."
    >
      <TextField
        label="Название"
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={60}
        autoFocus
      />
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <footer className="ht-foot">
        <span className="ht-foot__right">
          <Button appearance="ghost" onClick={onClose} disabled={pending}>
            Отмена
          </Button>
          <Button
            appearance="primary"
            loading={pending}
            disabled={name.trim() === ''}
            onClick={() => void create()}
          >
            Создать
          </Button>
        </span>
      </footer>
    </Sheet>
  );
};
