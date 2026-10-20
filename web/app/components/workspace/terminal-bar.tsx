// The thin bar under the agent conversation: the agent's folder, and its terminal (the agent's own session) unfolded
// above the bar on demand, for what only works there.
import { ChevronUp, SquareTerminal } from 'lucide-react';
import { TerminalView } from '@/terminal/terminal-view';
import { cn } from '@/lib/utils';
import './terminal-bar.css';
import { t } from '@/lib/i18n';

interface ITerminalBarProps {
  sessionName: string;
  cwd: string | null;
  open: boolean;
  onToggle: () => void;
}

export default function TerminalBar({ sessionName, cwd, open, onToggle }: ITerminalBarProps) {
  return (
    <div className={cn('hl-termbar', open && 'hl-termbar-open')} data-testid="terminal-bar">
      {open && (
        <div className="hl-termbar-screen">
          <TerminalView session={sessionName} focus />
        </div>
      )}
      <button
        type="button"
        className="hl-termbar-row"
        data-testid="terminal-bar-toggle"
        aria-expanded={open}
        onClick={onToggle}
      >
        <SquareTerminal size={12} aria-hidden />
        <span className="hl-termbar-label">{t('workspace.terminalBar.terminal')}</span>
        {cwd && <span className="hl-termbar-cwd">{cwd}</span>}
        <ChevronUp size={13} className="hl-termbar-chevron" aria-hidden />
      </button>
    </div>
  );
}
