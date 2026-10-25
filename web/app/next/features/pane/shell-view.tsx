// An own terminal of the chat (.hx-sh): a shell on the machine in the chat's folder, the agent does not see it. The
// body is xterm over the same socket as the agent's terminal, with the terminal's id.
import { useRef } from 'react';
import { useTerminalSocket } from '@/next/features/terminal/xterm-socket';

export interface IShellViewProps {
  /** The socket's address without the size. */
  url: string;
}

const ShellView = ({ url }: IShellViewProps) => {
  const bodyRef = useRef<HTMLDivElement>(null);
  useTerminalSocket(true, url, bodyRef);
  return (
    <div className="hx-tv" data-tool="shell">
      <div className="hx-sh">
        <div className="hx-sh__term" ref={bodyRef} />
      </div>
    </div>
  );
};

export default ShellView;
