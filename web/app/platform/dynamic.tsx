// next/dynamic on top of React.lazy. The loader may resolve to a module with a default export or to a component.
import { lazy, Suspense, type ComponentType, type ReactNode } from 'react';

type TLoaded<P> = ComponentType<P> | { default: ComponentType<P> };

interface IOptions {
  ssr?: boolean;
  loading?: (props: { error?: Error | null; isLoading?: boolean; pastDelay?: boolean }) => ReactNode;
}

export default function dynamic<P extends object>(
  loader: () => Promise<TLoaded<P>>,
  options: IOptions = {},
): ComponentType<P> {
  const Lazy = lazy(async () => {
    const loaded = await loader();
    return {
      default:
        typeof loaded === 'object' && loaded !== null && 'default' in loaded
          ? loaded.default
          : (loaded as ComponentType<P>),
    };
  });
  const fallback = options.loading
    ? options.loading({ isLoading: true, pastDelay: true, error: null })
    : null;
  function Dynamic(props: P) {
    return (
      <Suspense fallback={fallback}>
        <Lazy {...props} />
      </Suspense>
    );
  }
  return Dynamic;
}
