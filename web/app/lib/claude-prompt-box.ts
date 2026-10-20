const PROMPT_BOX_FOOTER_RE = /\? for shortcuts|bypass permissions|accept edits|plan mode|shift\+tab/;
/** The launch line the app types (`claude … --settings <hooks.json> …`, `claude --resume <id> …`). */
const LAUNCH_LINE_RE = /--settings\s|claude\s+--resume\b|--dangerously-skip-permissions/;

const tailLines = (pane: string): string[] =>
  pane
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(-8);

/** Claude Code draws its footer under the prompt box once the box takes input; notices and the agents list can sit below it. */
export const isPromptBoxReady = (pane: string): boolean =>
  PROMPT_BOX_FOOTER_RE.test(tailLines(pane).join(' '));

/**
 * The prompt box of the agent that was just launched. A pane keeps the screen of the Claude that ran there
 * before: its footer stays above the shell and the new launch line, and `isPromptBoxReady` takes it for the new agent.
 * The new agent draws below the line that launched it, so the footer counts only when it comes after that line.
 */
export const isLaunchedPromptBoxReady = (pane: string): boolean => {
  const lines = tailLines(pane);
  const footer = lines.findLastIndex((line) => PROMPT_BOX_FOOTER_RE.test(line));
  return footer >= 0 && lines.findLastIndex((line) => LAUNCH_LINE_RE.test(line)) < footer;
};
