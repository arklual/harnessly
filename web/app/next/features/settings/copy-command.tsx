// A command to run in a terminal, with a button that copies it; the button says so for a moment.
import { useEffect, useRef, useState } from 'react';
import { SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

const COPIED_MS = 1200;

const CopyCommand = ({ command }: { command: string }) => {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );
  const copy = () => {
    void navigator.clipboard.writeText(command).then(() => {
      setCopied(true);
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), COPIED_MS);
    });
  };
  return (
    <div className="hx-set__command">
      <code className="hx-set__command-text">{command}</code>
      <SetButton tone="ghost" icon={copied ? 'check' : 'copy'} onClick={copy}>
        {t(copied ? 'next.settings.design.copied' : 'next.settings.design.copy')}
      </SetButton>
    </div>
  );
};

export default CopyCommand;
