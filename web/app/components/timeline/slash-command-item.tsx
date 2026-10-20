import { memo, useId, useState } from 'react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, TwSpinner } from '@/components/agents/ui';
import type { ITimelineCommandOutput, ITimelineSlashCommand } from '@/types/timeline';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { isShellCommand } from '@/lib/hl-command-output';
import { plural, t } from '@/lib/i18n';

const COLLAPSED_LINES = 12;

const pluralLines = (n: number): string => plural(n, 'timeline.slashCommandItem.lines');

const CommandOutput = ({ output, isError }: { output: string; isError: boolean }) => {
  const holdPosition = useHoldTimelinePosition();
  const [expanded, setExpanded] = useState(false);
  const outputId = useId();
  const lines = output.split('\n');
  const collapsible = lines.length > COLLAPSED_LINES;
  const shown = collapsible && !expanded ? lines.slice(0, COLLAPSED_LINES).join('\n') : output;

  return (
    <div className="tw-cmd-body">
      <pre
        id={outputId}
        className="tw-cmd-output"
        data-error={isError || undefined}
        data-clipped={(collapsible && !expanded) || undefined}
      >
        {shown}
      </pre>
      {collapsible && (
        <TwButton
          variant={BUTTON_VARIANT.ghost}
          size={BUTTON_SIZE.sm}
          className="tw-cmd-toggle"
          aria-expanded={expanded}
          aria-controls={outputId}
          onClick={() => {
            holdPosition();
            setExpanded((v) => !v);
          }}
        >
          {expanded
            ? t('timeline.slashCommandItem.collapse')
            : t('timeline.slashCommandItem.showAll', { lines: pluralLines(lines.length) })}
        </TwButton>
      )}
    </div>
  );
};

interface ISlashCommandItemProps {
  entry: ITimelineSlashCommand | ITimelineCommandOutput;
}

/**
 * A `!` line run in the agent's shell, drawn like Claude Code's bash mode: the command in a terminal card,
 * a sweeping line while it runs, its output sliding in once the transcript has it.
 */
const ShellCommandItem = ({ entry }: { entry: ITimelineSlashCommand }) => {
  let state = 'done';
  if (entry.pending || entry.output === undefined) state = 'running';
  else if (entry.isError) state = 'error';
  return (
    <div className="tw-shell" data-state={state}>
      <div className="tw-shell-head">
        <span className="tw-shell-bang" aria-hidden="true">
          !
        </span>
        <code className="tw-shell-command">{entry.args}</code>
        {state === 'running' && (
          <span className="tw-shell-status" role="status">
            {t('timeline.slashCommandItem.running')}
          </span>
        )}
        {state === 'error' && (
          <span className="tw-shell-status">{t('timeline.slashCommandItem.failed')}</span>
        )}
      </div>
      {entry.output !== undefined && entry.output !== '' && (
        <div className="tw-shell-out">
          <CommandOutput output={entry.output} isError={entry.isError === true} />
        </div>
      )}
      {entry.output === '' && (
        <div className="tw-shell-empty">{t('timeline.slashCommandItem.doneNoOutput')}</div>
      )}
    </div>
  );
};

const SlashCommandItem = ({ entry }: ISlashCommandItemProps) => {
  if (entry.type === 'slash-command' && isShellCommand(entry)) return <ShellCommandItem entry={entry} />;
  if (entry.type === 'command-output') {
    return (
      <div className="tw-cmd" data-state={entry.isError ? 'error' : 'done'}>
        <div className="tw-cmd-head">
          <span className="tw-cmd-glyph" aria-hidden="true">
            /
          </span>
          <span className="tw-cmd-caption">{t('timeline.slashCommandItem.commandOutput')}</span>
        </div>
        <CommandOutput output={entry.output ?? ''} isError={entry.isError ?? false} />
      </div>
    );
  }

  const isError = entry.isError === true;
  let state = 'done';
  if (entry.pending) state = 'pending';
  else if (isError) state = 'error';

  return (
    <div className="tw-cmd" data-state={state}>
      <div className="tw-cmd-head">
        <span className="tw-cmd-glyph" aria-hidden="true">
          /
        </span>
        <span className="tw-cmd-name">{entry.name}</span>
        {entry.args && <span className="tw-cmd-args">{entry.args}</span>}
        {entry.pending && (
          <span className="tw-cmd-status" role="status">
            <TwSpinner />
            {t('timeline.slashCommandItem.running')}
          </span>
        )}
        {isError && <span className="tw-cmd-status">{t('timeline.slashCommandItem.failed')}</span>}
      </div>
      {entry.output && <CommandOutput output={entry.output} isError={isError} />}
    </div>
  );
};

export default memo(SlashCommandItem);
