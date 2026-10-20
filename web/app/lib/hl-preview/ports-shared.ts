/** The part of the port list the browser needs too: no node imports here. */
export type TPortGroup = 'chat' | 'agent' | 'other';
export type TPortKind = 'site' | 'api' | 'unknown';

export interface IPreviewPort {
  port: number;
  pid: number | null;
  command: string | null;
  cwd: string | null;
  group: TPortGroup;
  owner: { repo: string; task: string } | null;
  kind: TPortKind;
  responding: boolean;
}

const MAX_RELATED = 7;

export const relatedPorts = (all: IPreviewPort[], port: number): number[] => {
  const target = all.find((candidate) => candidate.port === port);
  if (!target || target.group === 'other') return [];
  const sameGroup = (candidate: IPreviewPort) =>
    target.group === 'chat'
      ? candidate.group === 'chat'
      : candidate.group === 'agent' &&
        candidate.owner?.repo === target.owner?.repo &&
        candidate.owner?.task === target.owner?.task;
  // An MFE neighbour is a server that answers: a silent or unprobed port would only add a dead path to the claim.
  return all
    .filter(
      (candidate) =>
        candidate.port !== port &&
        candidate.responding &&
        candidate.kind !== 'unknown' &&
        sameGroup(candidate),
    )
    .map((candidate) => candidate.port)
    .slice(0, MAX_RELATED);
};
