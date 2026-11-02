// Команды и лимиты (HRN-540): people by team with their roles and limits, the sheets to change them, the journal.
// A lead sees only their team («Моя команда»). Whatever the role may not change is disabled and says who can.
import '../teams.css';

import { useMemo, useState } from 'react';
import { Gauge, Mail, Plus, Search, Users, UsersRound, Wallet } from 'lucide-react';

import { Button } from '@kit';

import { useNow } from '../../lib/hooks';
import { Link } from '../../lib/router';
import type { TeamsView } from '../api/teams';
import { useOrg } from '../context';
import { useLoad } from '../hooks';
import { Loading, LoadError, Page, PageHead, Seg } from '../ui/page';
import { MiniBars, ProgressLine, Tile } from '../ui/tile';
import { Journal } from './teams/journal';
import { kRub, levelOf, people as peopleText, plural, sharePct, thousand } from './teams/format';
import { NewTeamSheet, PersonSheet, ScopeSheet, TeamSheet } from './teams/sheets';
import { FlatPeople, TeamSection } from './teams/table';

type View = 'teams' | 'list';

const Tiles = ({ v }: { v: TeamsView }) => {
  const all = v.teams.flatMap((t) => t.people);
  const near = all.filter((p) => (sharePct(p.spentRub, p.effective.amountRub) ?? 0) >= 80);
  const hottest = v.teams
    .map((t) => ({ t, pct: sharePct(t.spentRub, t.limitRub) }))
    .filter((x): x is { t: (typeof v.teams)[number]; pct: number } => x.pct !== null)
    .sort((a, b) => b.pct - a.pct)[0];
  const spentPct = sharePct(v.scope.spentRub, v.scope.limitRub);
  return (
    <div className="ho-tiles">
      <Tile
        tone="violet"
        icon={<Wallet size={15} />}
        title="Расход за месяц"
        viz={<ProgressLine value={(spentPct ?? 0) / 100} />}
        line={
          v.scope.limitRub === null
            ? `${kRub(v.scope.spentRub)}${v.spendKnown ? '' : ' · учтено только по лимитам'}`
            : `${thousand(v.scope.spentRub)} из ${kRub(v.scope.limitRub)} — лимит ${v.scope.label.toLowerCase().startsWith('команда') ? 'команды' : 'тенанта'}`
        }
      />
      <Tile
        tone="teal"
        icon={<Users size={15} />}
        title="Активны"
        viz={<MiniBars values={v.activeSeries} />}
        line={`${v.activeWeek} из ${v.total} ${plural(v.total, 'человека', 'человек', 'человек')} на этой неделе`}
      />
      <Tile
        tone="amber"
        icon={<Gauge size={15} />}
        title="Близко к лимиту"
        viz={<ProgressLine value={all.length ? near.length / all.length : 0} />}
        line={
          near.length === 0 && !hottest
            ? 'никто'
            : [
                hottest && levelOf(hottest.pct) !== 'ok' ? `${hottest.t.name} — ${hottest.pct}%` : null,
                near.length > 0 ? `${peopleText(near.length)} за 80%` : null,
              ]
                .filter(Boolean)
                .join(' · ') || 'никто'
        }
      />
      <Tile
        tone="gray"
        icon={<Mail size={15} />}
        title="Не работают"
        viz={<ProgressLine value={v.total ? (v.notAccepted + v.notVisiting) / v.total : 0} />}
        line={
          v.notAccepted + v.notVisiting === 0
            ? 'все на связи'
            : [
                v.notAccepted ? `${v.notAccepted} не приняли приглашение` : null,
                v.notVisiting ? `${v.notVisiting} не заходят неделю` : null,
              ]
                .filter(Boolean)
                .join(' · ')
        }
      />
    </div>
  );
};

export const TeamsScreen = () => {
  const { scope, shell } = useOrg();
  const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const lead = shell.viewer.role === 'lead';
  const { data, error, reload } = useLoad(
    (api) => api.teamsView(shell.org.slug, slug),
    [shell.org.slug, slug],
  );
  // A mutation answers with the new view: it replaces the loaded one until the scope changes.
  const [fresh, setFresh] = useState<{ key: string; view: TeamsView } | null>(null);
  const [rev, setRev] = useState(0);
  const view = fresh && fresh.key === slug ? fresh.view : data;
  const now = useNow(60_000);
  const [mode, setMode] = useState<View>('teams');
  const [q, setQ] = useState('');
  const [opened, setOpened] = useState<Record<string, boolean>>({});
  const [sheet, setSheet] = useState<{ kind: 'person' | 'team' | 'scope' | 'new'; id: string } | null>(null);
  const [kept, setKept] = useState(sheet);
  if (sheet && sheet !== kept) setKept(sheet);
  const done = (v: TeamsView) => {
    setFresh({ key: slug, view: v });
    setRev((n) => n + 1);
  };

  const needle = q.trim().toLowerCase();
  const teams = useMemo(() => {
    if (!view) return null;
    return view.teams
      .map((t) => ({
        ...t,
        people: needle
          ? t.people.filter(
              (p) => p.name.toLowerCase().includes(needle) || t.name.toLowerCase().includes(needle),
            )
          : t.people,
      }))
      .filter((t) => !needle || t.people.length > 0);
  }, [view, needle]);

  const personHit =
    kept?.kind === 'person'
      ? view?.teams.flatMap((t) => t.people.map((p) => ({ p, t }))).find((x) => x.p.id === kept.id)
      : undefined;
  const teamHit = kept?.kind === 'team' ? view?.teams.find((t) => t.id === kept.id) : undefined;
  const close = () => setSheet(null);

  const title = lead ? 'Моя команда' : 'Команды';
  const sub = view
    ? lead
      ? `${view.scope.label} · ${peopleText(view.total)}${view.scope.limitRub !== null ? ` · лимит команды ${kRub(view.scope.limitRub)} в месяц` : ''}`
      : `${view.scope.label} · ${view.teams.filter((t) => t.id !== null).length} ${plural(view.teams.filter((t) => t.id !== null).length, 'команда', 'команды', 'команд')} и ${peopleText(view.total)}${view.scope.limitRub !== null ? ` · лимит тенанта ${kRub(view.scope.limitRub)} в месяц` : ''}`
    : '';

  return (
    <Page>
      <PageHead
        title={title}
        sub={sub}
        actions={
          view && (
            <>
              {view.can.invite && scope.kind === 'tenant' && (
                <Link
                  to={`/s/${view.spaceSlug}/settings`}
                  className="k-btn"
                  data-appearance="outline"
                  data-size="s"
                >
                  <span className="k-btn__content">
                    <Mail size={14} aria-hidden="true" /> Пригласить
                  </span>
                </Link>
              )}
              {view.can.createTeam && (
                <Button
                  appearance="primary"
                  icon={<Plus size={14} />}
                  onClick={() => setSheet({ kind: 'new', id: '' })}
                >
                  Новая команда
                </Button>
              )}
            </>
          )
        }
      />
      {error != null ? (
        <LoadError error={error} retry={reload} />
      ) : !view || !teams ? (
        <Loading />
      ) : (
        <>
          <Tiles v={view} />
          <div className="ht-bar">
            <label className="ht-search">
              <Search size={14} aria-hidden="true" />
              <input
                type="search"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder={lead ? 'Найти человека' : 'Найти человека или команду'}
                aria-label="Поиск"
              />
            </label>
            <Button icon={<UsersRound size={14} />} onClick={() => setSheet({ kind: 'scope', id: '' })}>
              {lead ? 'Лимит команды' : 'Лимит тенанта'}
            </Button>
            <Seg
              label="Вид"
              value={mode}
              onChange={setMode}
              options={[
                { value: 'teams', label: 'По командам' },
                { value: 'list', label: 'Списком' },
              ]}
            />
          </div>
          {teams.length === 0 ? (
            <p className="ht-empty">{needle ? 'Ничего не нашлось.' : 'Здесь пока никого нет.'}</p>
          ) : mode === 'list' ? (
            <FlatPeople teams={teams} now={now} onOpenPerson={(id) => setSheet({ kind: 'person', id })} />
          ) : (
            <div className="ht-teams">
              {teams.map((t) => {
                const key = t.id ?? 'none';
                const dflt = !!needle || lead;
                return (
                  <TeamSection
                    key={key}
                    t={t}
                    open={opened[key] ?? dflt}
                    now={now}
                    onToggle={() => setOpened({ ...opened, [key]: !(opened[key] ?? dflt) })}
                    onOpenPerson={(id) => setSheet({ kind: 'person', id })}
                    onOpenTeam={(id) => setSheet({ kind: 'team', id })}
                  />
                );
              })}
            </div>
          )}
          <Journal slug={slug} rev={rev} now={now} />
          <PersonSheet
            p={personHit?.p ?? null}
            team={personHit?.t ?? null}
            teams={view.teams}
            open={sheet?.kind === 'person'}
            now={now}
            onClose={close}
            onDone={done}
          />
          <TeamSheet t={teamHit ?? null} open={sheet?.kind === 'team'} onClose={close} onDone={done} />
          <ScopeSheet
            key={`${view.scope.label}-${rev}`}
            s={view.scope}
            open={sheet?.kind === 'scope'}
            onClose={close}
            onDone={done}
          />
          <NewTeamSheet open={sheet?.kind === 'new'} onClose={close} onDone={done} />
        </>
      )}
    </Page>
  );
};
