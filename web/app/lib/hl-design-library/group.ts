import { t } from '@/lib/i18n';
import { stripControlChars } from '@/lib/terminal-safe-text';
import type { TCliState } from '@/types/timeline';
import type { ILibraryArtifact, ILibraryBoard } from '@/lib/hl-design-library/types';

/**
 * How the "Claude Design" tab lays the library out for one conversation: what its own agent works on,
 * what other agents work on, and the rest. Pure, shared by the tab, the overview island and the tests.
 */
export interface IArtifactGroups {
  mine: ILibraryArtifact[];
  others: ILibraryArtifact[];
  rest: ILibraryArtifact[];
}

export const groupArtifacts = (artifacts: readonly ILibraryArtifact[]): IArtifactGroups => {
  const groups: IArtifactGroups = { mine: [], others: [], rest: [] };
  for (const artifact of artifacts) {
    if (artifact.agents.some((agent) => agent.self)) groups.mine.push(artifact);
    else if (artifact.agents.length > 0) groups.others.push(artifact);
    else groups.rest.push(artifact);
  }
  return groups;
};

/** States in which the agent reads its conversation: "Message" is offered only then. */
export const isAgentRunning = (state: TCliState): boolean =>
  state !== 'inactive' && state !== 'cancelled' && state !== 'unknown';

export const AGENT_STATE_LABEL: Record<TCliState, string> = {
  busy: t('lib.designLibraryGroup.stateBusy'),
  'needs-input': t('lib.designLibraryGroup.stateNeedsInput'),
  'ready-for-review': t('lib.designLibraryGroup.stateReadyForReview'),
  idle: t('lib.designLibraryGroup.stateIdle'),
  inactive: t('lib.designLibraryGroup.stateInactive'),
  cancelled: t('lib.designLibraryGroup.stateCancelled'),
  unknown: t('lib.designLibraryGroup.stateUnknown'),
};

/** The board a card opens first: the overview board when the project has one, else the first. The pattern matches
 * overview board titles as Claude Design projects name them (English or Russian): data, not interface text. */
export const mainBoard = (artifact: ILibraryArtifact): ILibraryBoard | null =>
  artifact.boards.find(
    (board) => /^(все страницы|main|главная)$/i.test(board.title) || board.path === 'Main.dc.html',
  ) ??
  artifact.boards[0] ??
  null;

const flatten = (text: string): string =>
  text
    .replace(/\x1b/g, '␛')
    .replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, ' ')
    .replace(/\s+/g, ' ')
    // A trailing backslash makes Claude Code take Enter as a new line instead of sending.
    .replace(/[\s\\]+$/, '')
    .trim();

/**
 * One line for another agent's terminal: a newline would send half of it, a control sequence would be read as keys.
 * It says which design and where its files are, so the agent can pick the work up without asking.
 */
export const buildAgentMessage = (
  artifact: Pick<ILibraryArtifact, 'title' | 'projectPath'>,
  text: string,
): string =>
  t('lib.designLibraryGroup.agentMessage', {
    title: flatten(artifact.title),
    files: flatten(artifact.projectPath),
    text: flatten(text),
  });

const MAX_BLOCK_LINES = 400;

/**
 * Comments picked on a board (`<annotation>` blocks) for another agent's terminal: pasted as they are, with the line
 * naming the design and its files above them. The blocks are already cleaned by the formatter that wrote them; a
 * control sequence that slipped in is neutralised here too, before `sendBracketedPaste` pastes the text.
 */
export const buildAgentBlockMessage = (
  artifact: Pick<ILibraryArtifact, 'title' | 'projectPath'>,
  blocks: string,
): string =>
  `${t('lib.designLibraryGroup.agentBlockHead', { title: flatten(artifact.title), files: flatten(artifact.projectPath) })}\n${stripControlChars(blocks).trim().split('\n').slice(0, MAX_BLOCK_LINES).join('\n')}`;
