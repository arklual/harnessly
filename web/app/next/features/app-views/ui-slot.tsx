// A view of an app in a UI slot (settings page, onboarding step, object action, start page): the same sandboxed frame and bridge as
// every app view, inside a boundary of its own. A view that throws while it draws, or that does not open in time, is
// replaced by a line that says so; the page around it keeps working.
import { Component, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import AppFrame from '@/next/features/app-views/app-frame';
import type { IAppView, IInstalledApp } from '@/next/features/app-views/api';
import type { IBridgeHandlers } from '@/next/features/app-views/bridge';
import type { ISlotObject, TSlot } from '@/next/features/app-views/slots';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import LoadFailed from '@/next/ui/load-failed';

interface IBoundaryProps {
  onRetry: () => void;
  children: ReactNode;
}

export class SlotBoundary extends Component<IBoundaryProps, { failure: Error | null }> {
  state = { failure: null as Error | null };

  static getDerivedStateFromError(failure: Error) {
    return { failure };
  }

  render() {
    const { failure } = this.state;
    if (!failure) return this.props.children;
    return (
      <LoadFailed
        size="inline"
        title={t('next.slot.failed')}
        reason={failure.message}
        onRetry={() => {
          this.setState({ failure: null });
          this.props.onRetry();
        }}
      />
    );
  }
}

export interface IUiSlotProps {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  target: TSlot;
  viewPort: number;
  /** For an object action: the object the button is about; the view reads it as `hl.context.data.object`. */
  object?: ISlotObject;
  /** An onboarding step calls hl.slot.done(). */
  onDone?: () => void;
  /** A view of the start page calls hl.slot.greet() to put its own greeting over the field. */
  onGreet?: (title: string, sub: string) => void;
  /** The view fills the place it is given (the start page's stage) instead of growing to its own height. */
  fill?: boolean;
}

const UiSlot = ({
  machine,
  app,
  view,
  target,
  viewPort,
  object,
  onDone,
  onGreet,
  fill = false,
}: IUiSlotProps) => {
  const [reload, setReload] = useState(0);
  // The bridge is made anew whenever the handlers change, and a new bridge forgets the view's handshake: keep them still.
  const done = useRef(onDone);
  const greet = useRef(onGreet);
  useEffect(() => {
    done.current = onDone;
    greet.current = onGreet;
  });
  const handlers = useMemo<IBridgeHandlers>(
    () => ({
      slotDone: () => done.current?.(),
      ...(target === 'home'
        ? { slotGreet: (title: string, sub: string) => greet.current?.(title, sub) }
        : {}),
    }),
    [target],
  );
  const data = useMemo(() => (object ? { object } : undefined), [object]);
  return (
    <div
      className="hx-uislot"
      data-slot={target}
      data-app={app.id}
      data-view={view.id}
      data-fill={fill || undefined}
    >
      <SlotBoundary onRetry={() => setReload((n) => n + 1)}>
        <AppFrame
          machine={machine}
          app={app}
          view={view}
          target={target}
          viewPort={viewPort}
          conversation={null}
          data={data}
          handlers={handlers}
          reloadToken={reload}
          fit={!fill}
        />
      </SlotBoundary>
    </div>
  );
};

export default UiSlot;
