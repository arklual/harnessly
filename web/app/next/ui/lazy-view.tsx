// A screen or a tool that arrives in its own chunk (React.lazy), with the two ways it can fail told: while
// the chunk comes the fallback shows, and a chunk that does not come — a link that dropped, a build that moved on — is
// said with a «Повторить» that fetches it again, instead of a blank page. Nothing else the page shows is touched.
import {
  Component,
  createElement,
  lazy,
  Suspense,
  useState,
  type ComponentType,
  type LazyExoticComponent,
  type ReactNode,
} from 'react';
import LoadFailed, { type TLoadFailedSize } from '@/next/ui/load-failed';

interface IBoundaryProps {
  title: string;
  size: TLoadFailedSize;
  /** Shown under the message (the file's plain text under a failed editor). */
  below: ReactNode;
  onRetry: () => void;
  /** Tells the failure elsewhere (a toast) instead of drawing it here; for what has no place of its own on the page. */
  onFail?: (failure: Error, retry: () => void) => void;
  children: ReactNode;
}

class Boundary extends Component<IBoundaryProps, { failure: Error | null }> {
  state = { failure: null as Error | null };

  static getDerivedStateFromError(failure: Error) {
    return { failure };
  }

  componentDidCatch(failure: Error) {
    this.props.onFail?.(failure, this.props.onRetry);
  }

  render() {
    const { failure } = this.state;
    if (!failure) return this.props.children;
    if (this.props.onFail) return this.props.below;
    return (
      <>
        <LoadFailed
          size={this.props.size}
          title={this.props.title}
          reason={failure.message}
          onRetry={this.props.onRetry}
        />
        {this.props.below}
      </>
    );
  }
}

type TLoad<P> = () => Promise<{ default: ComponentType<P> }>;

export interface ILazyViewProps<P> {
  /** The import of the module whose default export is the view. */
  load: TLoad<P>;
  props: P;
  /** What shows while the chunk comes. */
  fallback: ReactNode;
  /** What the failure is called («Экран не открылся»). */
  title: string;
  size?: TLoadFailedSize;
  below?: ReactNode;
  onFail?: (failure: Error, retry: () => void) => void;
}

// One lazy component per import, kept for the page: a view that was loaded draws at once the next time, with no
// fallback in between. A retry replaces it, because a rejected import is not asked twice by the same lazy component.
const views = new WeakMap<object, unknown>();

const viewOf = <P,>(load: TLoad<P>): LazyExoticComponent<ComponentType<P>> => {
  const have = views.get(load) as LazyExoticComponent<ComponentType<P>> | undefined;
  if (have) return have;
  const made = lazy(load);
  views.set(load, made);
  return made;
};

const renew = <P,>(load: TLoad<P>): void => {
  views.delete(load);
};

const LazyView = <P extends object>({
  load,
  props,
  fallback,
  title,
  size = 'pane',
  below = null,
  onFail,
}: ILazyViewProps<P>) => {
  const [round, setRound] = useState(0);
  const View = viewOf(load);
  return (
    <Boundary
      key={round}
      title={title}
      size={size}
      below={below}
      onFail={onFail}
      onRetry={() => {
        renew(load);
        setRound((n) => n + 1);
      }}
    >
      <Suspense fallback={fallback}>{createElement(View, props)}</Suspense>
    </Boundary>
  );
};

export default LazyView;
