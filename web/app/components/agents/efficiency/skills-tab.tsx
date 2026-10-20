import { useMemo, useState } from 'react';
import { ChevronDown, ChevronRight, Copy } from 'lucide-react';
import { toast } from 'sonner';
import { useEfficiency } from '@/hooks/use-efficiency';
import type { IEffListedSkill, IEffSkillInventory, TEffScope } from '@/types/efficiency';
import { compact, n0, pct, shortDay } from '@/components/agents/efficiency/format';
import { Card, Chip, Empty, Rich, Skeleton } from '@/components/agents/efficiency/primitives';
import Gate from '@/components/agents/efficiency/gate';
import { plural, t } from '@/lib/i18n';

const OWN = t('efficiency.skillsTab.own');

type TVerdict = 'off' | 'trim' | 'hidden' | 'works' | 'gone';

const VERDICT: Record<TVerdict, { label: string; tone: 'bad' | 'warn' | 'ok'; hint: string; order: number }> =
  {
    off: {
      label: t('efficiency.skillsTab.offLabel'),
      tone: 'bad',
      hint: t('efficiency.skillsTab.offHint'),
      order: 0,
    },
    trim: {
      label: t('efficiency.skillsTab.trimLabel'),
      tone: 'warn',
      hint: t('efficiency.skillsTab.trimHint'),
      order: 1,
    },
    hidden: {
      label: t('efficiency.skillsTab.hiddenLabel'),
      tone: 'warn',
      hint: t('efficiency.skillsTab.hiddenHint'),
      order: 2,
    },
    works: {
      label: t('efficiency.skillsTab.worksLabel'),
      tone: 'ok',
      hint: t('efficiency.skillsTab.worksHint'),
      order: 3,
    },
    gone: {
      label: t('efficiency.skillsTab.goneLabel'),
      tone: 'ok',
      hint: t('efficiency.skillsTab.goneHint'),
      order: 4,
    },
  };

interface IPluginRow {
  plugin: string;
  skills: IEffListedSkill[];
  used: number;
  loads: number;
  chars: number;
  described: number;
  verdict: TVerdict;
  lastShown: string;
}

/** A plugin no session has been shown for this many days is treated as switched off. */
const GONE_DAYS = 3;

/** Listing characters are roughly a quarter of a token each. */
const tokensOf = (chars: number) => chars / 4;

const groupByPlugin = (inv: IEffSkillInventory): IPluginRow[] => {
  const latest = inv.skills.reduce((m, s) => (s.lastShownDay > m ? s.lastShownDay : m), '');
  const cutoff = latest
    ? new Date(Date.parse(latest) - (GONE_DAYS - 1) * 86_400_000).toISOString().slice(0, 10)
    : '';
  const map = new Map<string, IEffListedSkill[]>();
  for (const s of inv.skills) map.set(s.plugin ?? OWN, [...(map.get(s.plugin ?? OWN) ?? []), s]);
  return [...map.entries()]
    .map(([plugin, skills]) => {
      const used = skills.filter((s) => s.loads > 0).length;
      const described = skills.reduce((sum, s) => sum + s.descriptionShare, 0) / skills.length;
      const lastShown = skills.reduce((m, s) => (s.lastShownDay > m ? s.lastShownDay : m), '');
      let verdict: TVerdict = 'works';
      if (lastShown < cutoff) verdict = 'gone';
      else if (used === 0) verdict = plugin.startsWith('hl-') ? 'hidden' : 'off';
      else if (used / skills.length < 0.3 && skills.length >= 4) verdict = 'trim';
      else if (described < 0.5) verdict = 'hidden';
      return {
        plugin,
        skills,
        used,
        loads: skills.reduce((sum, s) => sum + s.loads, 0),
        chars: skills.reduce((sum, s) => sum + s.avgListingChars, 0),
        described,
        verdict,
        lastShown,
      };
    })
    .sort((a, b) => VERDICT[a.verdict].order - VERDICT[b.verdict].order || b.chars - a.chars);
};

const SkillRows = ({ skills }: { skills: IEffListedSkill[] }) => (
  <tr>
    <td colSpan={6} className="p-0">
      <div className="ts-table-wrap">
        <table className="ts-table">
          <thead>
            <tr>
              <th>{t('efficiency.skillsTab.colSkill')}</th>
              <th className="ts-num">{t('efficiency.skillsTab.colShown')}</th>
              <th className="ts-num">{t('efficiency.skillsTab.colLoaded')}</th>
              <th>{t('efficiency.skillsTab.colLastTime')}</th>
              <th className="ts-num">{t('efficiency.skillsTab.colDescription')}</th>
            </tr>
          </thead>
          <tbody>
            {skills.map((s) => (
              <tr key={s.skill}>
                <td className="font-mono text-[12px]">{s.skill}</td>
                <td className="ts-num">{n0(s.sessionsShown)}</td>
                <td className="ts-num">
                  {s.loads ? (
                    n0(s.loads)
                  ) : (
                    <span className="text-(--t-status-danger)">{t('efficiency.skillsTab.never')}</span>
                  )}
                </td>
                <td>{s.lastLoadedDay ? shortDay(s.lastLoadedDay) : '—'}</td>
                <td className="ts-num">{pct(s.descriptionShare)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </td>
  </tr>
);

const Body = ({ inv }: { inv: IEffSkillInventory }) => {
  const rows = useMemo(() => groupByPlugin(inv), [inv]);
  const [open, setOpen] = useState<string | null>(null);
  const off = rows.filter((r) => r.verdict === 'off' && r.plugin !== OWN);
  const offTokens = tokensOf(off.reduce((sum, r) => sum + r.chars, 0));
  const never = inv.totals.listed - inv.totals.loaded;
  const gone = rows.filter((r) => r.verdict === 'gone');
  const latest = inv.skills.reduce((m, sk) => (sk.lastShownDay > m ? sk.lastShownDay : m), '');
  if (inv.totals.listed === 0) return <Empty text={t('efficiency.skillsTab.empty')} />;
  const copy = async () => {
    const text = off.map((r) => r.plugin).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t('efficiency.skillsTab.copied'));
    } catch {
      toast.error(t('efficiency.skillsTab.copyFailed'));
    }
  };
  return (
    <>
      <section className="ts-panel mb-4">
        <p className="text-[15px] leading-relaxed">
          <Rich
            text={t('efficiency.skillsTab.lead', {
              listed: plural(inv.totals.listed, 'efficiency.skillsTab.listedSkills'),
              share: pct(inv.totals.listed ? inv.totals.loaded / inv.totals.listed : null, 1),
            })}
            parts={{
              listedCount: <b className="tabular-nums">{n0(inv.totals.listed)}</b>,
              loaded: <b className="tabular-nums">{n0(inv.totals.loaded)}</b>,
              never: <b className="tabular-nums">{n0(never)}</b>,
              tokens: (
                <b className="tabular-nums">{compact(tokensOf(inv.totals.neverLoadedCharsPerSession))}</b>
              ),
            }}
          />
        </p>
        <p className="mt-1.5 text-[13px] text-(--t-text-secondary)">
          {t('efficiency.skillsTab.leadNote', { sessions: n0(inv.totals.sessions) })}
        </p>
      </section>

      <Card
        title={plural(off.length, 'efficiency.skillsTab.offTitle')}
        sub={[
          off.length
            ? t('efficiency.skillsTab.offSub', { tokens: compact(offTokens) })
            : t('efficiency.skillsTab.offNone'),
          gone.length ? t('efficiency.skillsTab.goneNote', { count: gone.length, days: GONE_DAYS }) : '',
          t('efficiency.skillsTab.snapshotNote', { day: latest ? shortDay(latest) : '—' }),
        ]
          .filter(Boolean)
          .join(' ')}
        actions={
          off.length ? (
            <button
              type="button"
              className="ts-btn inline-flex items-center gap-1.5"
              onClick={() => void copy()}
            >
              <Copy className="size-3.5" aria-hidden="true" />
              {t('efficiency.skillsTab.copyList')}
            </button>
          ) : undefined
        }
      >
        {off.length > 0 && (
          <ul className="flex flex-wrap gap-2">
            {off.map((r) => (
              <li key={r.plugin}>
                <Chip tone="bad">
                  {r.plugin} · {n0(r.skills.length)} · {compact(tokensOf(r.chars))}
                </Chip>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card title={t('efficiency.skillsTab.allTitle')} sub={t('efficiency.skillsTab.allSub')}>
        <div className="ts-table-wrap">
          <table className="ts-table">
            <thead>
              <tr>
                <th>{t('efficiency.skillsTab.colPlugin')}</th>
                <th className="ts-num">{t('efficiency.skillsTab.colSkills')}</th>
                <th className="ts-num">{t('efficiency.skillsTab.colUsed')}</th>
                <th className="ts-num">{t('efficiency.skillsTab.colLoads')}</th>
                <th className="ts-num">{t('efficiency.skillsTab.colListingTokens')}</th>
                <th>{t('efficiency.skillsTab.colVerdict')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const isOpen = open === r.plugin;
                return [
                  <tr
                    key={r.plugin}
                    className="cursor-pointer"
                    tabIndex={0}
                    aria-expanded={isOpen}
                    onClick={() => setOpen(isOpen ? null : r.plugin)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') setOpen(isOpen ? null : r.plugin);
                    }}
                  >
                    <td className="whitespace-nowrap">
                      <span className="inline-flex items-center gap-1">
                        {isOpen ? (
                          <ChevronDown className="size-3.5" aria-hidden="true" />
                        ) : (
                          <ChevronRight className="size-3.5" aria-hidden="true" />
                        )}
                        <b>{r.plugin}</b>
                      </span>
                    </td>
                    <td className="ts-num">{n0(r.skills.length)}</td>
                    <td className="ts-num">{n0(r.used)}</td>
                    <td className="ts-num">{n0(r.loads)}</td>
                    <td className="ts-num">{compact(tokensOf(r.chars))}</td>
                    <td title={VERDICT[r.verdict].hint}>
                      <Chip tone={VERDICT[r.verdict].tone}>{VERDICT[r.verdict].label}</Chip>
                    </td>
                  </tr>,
                  isOpen ? <SkillRows key={`${r.plugin}-skills`} skills={r.skills} /> : null,
                ];
              })}
            </tbody>
          </table>
        </div>
      </Card>
    </>
  );
};

const SkillsTab = ({ scope }: { scope: TEffScope }) => {
  const inventory = useEfficiency<IEffSkillInventory>(`/skills?scope=${scope}&days=30`);
  return (
    <Gate
      resource={inventory}
      skeleton={
        <div className="flex flex-col gap-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-40" />
          <Skeleton className="h-96" />
        </div>
      }
    >
      {(inv) => <Body inv={inv} />}
    </Gate>
  );
};

export default SkillsTab;
