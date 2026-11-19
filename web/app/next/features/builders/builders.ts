// The hub's pool of builder machines for a heavy action of the page (a render button, later others): the list the runtime
// serves at /api/platform/builders (cmd/runtime/routes_builders.go) and the option each builder makes in a picker. The
// choice «auto» is the empty string: the hub then picks the least loaded builder that has the labels the job needs.
// HRN-1683, docs/hub/builders.md.

export interface Builder {
  id: string;
  name: string;
  caps: string[];
  slots: number;
  running: number;
  waiting: number;
  online: boolean;
  ready: boolean;
  note?: string;
}

export interface BuilderPool {
  /** The machine is in a hub; false means no pool at all (the job runs on this machine's own slots). */
  hub: boolean;
  builders: Builder[];
}

export const AUTO = '';

export interface BuilderOption {
  value: string;
  label: string;
  /** A builder that is offline or does not take jobs stays in the list, greyed, so the person sees why. */
  disabled: boolean;
}

/** The builders that carry every label the job needs (all of them when it needs none). */
export const fitting = (list: Builder[], caps: readonly string[]): Builder[] =>
  list.filter((b) => caps.every((c) => b.caps.includes(c.toLowerCase())));

/** «Авто» first, then each fitting builder with its load: «render-1 · 1 из 2 · blender, gpu». */
export function builderOptions(pool: BuilderPool, caps: readonly string[] = []): BuilderOption[] {
  const ready = fitting(pool.builders, caps).filter((b) => b.ready).length;
  const auto: BuilderOption = {
    value: AUTO,
    label: ready > 0 ? `Авто · свободный билдер (${ready})` : 'Авто · на этой машине (билдеров нет)',
    disabled: false,
  };
  const rest = fitting(pool.builders, caps).map((b) => ({
    value: b.id,
    label: `${b.name} · ${b.running} из ${b.slots}${b.ready ? '' : ` · ${b.note ?? 'не готов'}`} · ${b.caps.join(', ') || 'без меток'}`,
    disabled: !b.ready,
  }));
  return [auto, ...rest];
}

/** Reads the pool from the runtime; a runtime without the route or a failed read is a pool with no builders. */
export async function loadBuilders(fetcher: typeof fetch = fetch, base = ''): Promise<BuilderPool> {
  try {
    const res = await fetcher(`${base}/api/platform/builders`, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (!res.ok) return { hub: false, builders: [] };
    const body = (await res.json()) as Partial<BuilderPool>;
    return { hub: body.hub === true, builders: Array.isArray(body.builders) ? body.builders : [] };
  } catch {
    return { hub: false, builders: [] };
  }
}
