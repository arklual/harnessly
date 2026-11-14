// An agent changes the settings (internal/settingsctl) as the page sees it: the journal of the changes, the
// person's own change of a setting by its key (which is also how a change is taken back), and the proposals of the
// agents of ordinary chats that wait for the person's yes or no.
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { answer, list, num, obj, opt, str } from '@/next/features/settings/platform-answer';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const CHANGE_BY = { person: 'person', agent: 'agent' } as const;
export type TChangeBy = (typeof CHANGE_BY)[keyof typeof CHANGE_BY];

/** One line of the journal. */
export interface ISettingChange {
  id: string;
  /** Milliseconds. */
  at: number;
  key: string;
  title: string;
  /** The values as the catalog takes them, and the same in words. */
  old: string;
  new: string;
  oldLabel: string;
  newLabel: string;
  by: TChangeBy;
  /** The agent's chat as it was called then; '' for a person. */
  chat: string;
  /** The agent's one line of why; '' when it gave none. */
  why: string;
}

/** A change an agent of a chat asks the person to confirm. */
export interface ISettingProposal {
  id: string;
  key: string;
  title: string;
  oldLabel: string;
  newLabel: string;
  why: string;
  /** The chat whose agent asks: the card is shown there. */
  conv: string;
}

const readChange = (v: unknown): ISettingChange => {
  const o = obj(v, 'change');
  const by = obj(o.by, 'by');
  return {
    id: str(o.id, 'id'),
    at: num(o.at, 'at'),
    key: str(o.key, 'key'),
    title: str(o.title, 'title'),
    old: str(o.old, 'old'),
    new: str(o.new, 'new'),
    oldLabel: opt(o.oldLabel) || str(o.old, 'old'),
    newLabel: opt(o.newLabel) || str(o.new, 'new'),
    by: by.kind === CHANGE_BY.agent ? CHANGE_BY.agent : CHANGE_BY.person,
    chat: opt(by.title),
    why: opt(o.why),
  };
};

const readProposal = (v: unknown): ISettingProposal => {
  const o = obj(v, 'proposal');
  return {
    id: str(o.id, 'id'),
    key: str(o.key, 'key'),
    title: str(o.title, 'title'),
    oldLabel: opt(o.oldLabel) || str(o.old, 'old'),
    newLabel: opt(o.newLabel) || str(o.new, 'new'),
    why: opt(o.why),
    conv: str(o.conv, 'conv'),
  };
};

const url = (machine: MachineId, path: `/${string}`) => runtimePath(platformPath(path), machine);

/** The journal, the newest first. */
export const readSettingChanges = answer((o) => list(o.changes, 'changes', readChange));

export const loadSettingChanges = (machine: MachineId, limit: number) =>
  getPage(url(machine, `/settings-changes?limit=${limit}`), readSettingChanges, TIMEOUT_MS);

/** The person sets a setting of the catalog; the answer is the line the journal got. */
export const putSetting = (machine: MachineId, key: string, value: string) =>
  sendBody(
    'PUT',
    url(machine, `/settings-catalog/${encodeURIComponent(key)}`),
    { value },
    answer((o) => readChange(o.change)),
    TIMEOUT_MS,
  );

/** The proposals that wait for the person, of every chat of the machine. */
export const loadSettingProposals = (machine: MachineId) =>
  getPage(
    url(machine, '/settings-proposals'),
    answer((o) => list(o.proposals, 'proposals', readProposal)),
    TIMEOUT_MS,
  );

export const PROPOSAL_VERB = { approve: 'approve', decline: 'decline' } as const;
export type TProposalVerb = (typeof PROPOSAL_VERB)[keyof typeof PROPOSAL_VERB];

/** The person's yes or no; the answer only says the machine took it. */
export const answerSettingProposal = (machine: MachineId, id: string, verb: TProposalVerb) =>
  sendPage(
    'POST',
    url(machine, `/settings-proposals/${encodeURIComponent(id)}/${verb}`),
    answer(() => true),
    TIMEOUT_MS,
  );
