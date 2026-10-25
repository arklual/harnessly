// «Связь между агентами» of Settings → Machines, as rows: who may write to this machine's agents (this machine, the
// person's other machines, everyone who has written here, everyone a rule names), whether they may, and the audit in
// words. The runtime decides the same way (internal/peers/policy.go): a rule first, then the scope.
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { IPeerRecord, IPeerRule, IPeerSettings, IPeersView } from '@/next/features/peers/api';

export interface IPeerWhoRow {
  /** `<kind>:<id>`: the key of its pending change. */
  key: string;
  kind: IPeerRule['kind'];
  id: string;
  name: string;
  sub: string;
  allowed: boolean;
}

/** A machine of the person's fleet as the page knows it. */
export interface IFleetMachine {
  id: string;
  name: string;
}

const LOCAL = 'local';

const keyOf = (kind: IPeerRule['kind'], id: string) => `${kind}:${id.toLowerCase()}`;

/** Whether a machine or a person may write, as the runtime decides it. */
export const allowedFor = (settings: IPeerSettings, kind: IPeerRule['kind'], id: string): boolean => {
  const rule = settings.rules.find((r) => keyOf(r.kind, r.id) === keyOf(kind, id));
  if (rule) return rule.allow;
  return settings.scope === 'team';
};

export const peerWhoRows = (view: IPeersView, fleet: readonly IFleetMachine[]): IPeerWhoRow[] => {
  const rows = new Map<string, IPeerWhoRow>();
  const add = (kind: IPeerRule['kind'], id: string, name: string, sub: string) => {
    if (!id) return;
    const key = keyOf(kind, id);
    if (rows.has(key)) return;
    rows.set(key, { key, kind, id, name: name || id, sub, allowed: allowedFor(view.settings, kind, id) });
  };
  add('machine', view.self.machine, view.self.machineName, t('next.settings.peers.thisMachine'));
  // The fleet names the default machine `local`; its fleet id is known only for the machine this page asks.
  fleet
    .filter((machine) => machine.id !== LOCAL)
    .forEach((machine) => add('machine', machine.id, machine.name, t('next.settings.peers.yourMachine')));
  view.senders.forEach((sender) => {
    add(
      'machine',
      sender.machine,
      sender.machineName,
      t('next.settings.peers.machineOf', { who: sender.personName || sender.person }),
    );
    add('person', sender.person, sender.personName, sender.person);
  });
  view.settings.rules.forEach((rule) =>
    add(rule.kind, rule.id, rule.name, rule.kind === 'person' ? rule.id : t('next.settings.peers.machine')),
  );
  return [...rows.values()];
};

/** The settings with one machine or person allowed or blocked. */
export const withRule = (
  settings: IPeerSettings,
  row: Pick<IPeerWhoRow, 'kind' | 'id' | 'name'>,
  allow: boolean,
): IPeerSettings => {
  const key = keyOf(row.kind, row.id);
  const rules = settings.rules.filter((r) => keyOf(r.kind, r.id) !== key);
  return { ...settings, rules: [...rules, { kind: row.kind, id: row.id, name: row.name, allow }] };
};

const RULE_ID = /^[A-Za-z0-9._@+-]{1,128}$/;

/** What the person typed to add as a row: an email is a person, anything else a machine; null when it cannot be one. */
export const parseWho = (input: string): Pick<IPeerWhoRow, 'kind' | 'id' | 'name'> | null => {
  const id = input.trim();
  if (!RULE_ID.test(id)) return null;
  return { kind: id.includes('@') ? 'person' : 'machine', id, name: id };
};

export interface IPeerLogRow {
  id: string;
  dir: IPeerRecord['dir'];
  refused: boolean;
  route: string;
  text: string;
  when: string;
  why: string | null;
}

const partyLine = (p: IPeerRecord['from']) =>
  [p.personName || p.person, p.machineName || p.machine, p.title]
    .filter((part) => part.length > 0)
    .join(' · ');

export const peerLogRows = (audit: readonly IPeerRecord[], now: number): IPeerLogRow[] =>
  audit.map((r) => {
    const refused = r.state === 'refused';
    let why: string | null = null;
    if (refused) why = r.reason ? t(`next.peers.reason.${r.reason}`) : t('next.peers.notDelivered');
    return {
      id: r.id,
      dir: r.dir,
      refused,
      route: `${partyLine(r.from)} → ${partyLine(r.to)}`,
      text: r.text,
      when: formatAgo(r.at, now),
      why,
    };
  });
