// The generators a machine draws pictures with (`<platform>/images?providers=1`, cmd/runtime/images_media.go): Codex and
// Antigravity always, Higgsfield with its models when the person connected it. A paid one says its price before it
// draws (`POST <platform>/images?estimate=1`). The lists of a machine are read once a page and kept.
import { useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { isImageProvider, type ImageProvider } from '@/next/features/image-edit/host';
import type { MachineId } from '@/next/protocol/v2';

export interface IImageModel {
  id: string;
  name: string;
  maker?: string;
  /** Takes a reference picture: it may change a picture, not only draw a new one. */
  refs: boolean;
}

export interface IImageProviderInfo {
  id: ImageProvider;
  name: string;
  /** Each picture spends the person's balance at the provider. */
  paid: boolean;
  experimental: boolean;
  /** Draws a transparent background and removes one. */
  transparent: boolean;
  /** Changes a picture too. */
  edit: boolean;
  /** Its models, the default first; empty: no choice of model. */
  models: IImageModel[];
}

/** What a page offers before the machine has answered, or when it cannot: the generators every machine has. */
export const BASE_PROVIDERS: readonly IImageProviderInfo[] = [
  { id: 'codex', name: 'Codex', paid: false, experimental: false, transparent: true, edit: true, models: [] },
  {
    id: 'antigravity',
    name: 'Antigravity',
    paid: false,
    experimental: true,
    transparent: false,
    edit: true,
    models: [],
  },
];

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

const readModel = (v: unknown): IImageModel | null => {
  if (!isRecord(v) || typeof v.id !== 'string' || typeof v.name !== 'string') return null;
  return {
    id: v.id,
    name: v.name,
    ...(typeof v.maker === 'string' && v.maker !== '' ? { maker: v.maker } : {}),
    refs: v.refs === true,
  };
};

/** Reads the machine's answer field by field; a generator this page does not know is left out. */
export const readProviders = (body: unknown): IImageProviderInfo[] => {
  const list = isRecord(body) && Array.isArray(body.providers) ? body.providers : [];
  const out: IImageProviderInfo[] = [];
  for (const item of list) {
    if (!isRecord(item) || !isImageProvider(item.id)) continue;
    const models = Array.isArray(item.models)
      ? item.models.map(readModel).filter((m): m is IImageModel => m !== null)
      : [];
    out.push({
      id: item.id,
      name: typeof item.name === 'string' && item.name !== '' ? item.name : item.id,
      paid: item.paid === true,
      experimental: item.experimental === true,
      transparent: item.transparent === true,
      edit: item.edit === true,
      models,
    });
  }
  return out.some((p) => p.id === 'codex') ? out : [...BASE_PROVIDERS];
};

const asked = new Map<MachineId, Promise<IImageProviderInfo[]>>();

export const readImageProviders = (m: MachineId): Promise<IImageProviderInfo[]> => {
  let p = asked.get(m);
  if (!p) {
    p = runtimeJson<unknown>(m, platformPath('/images?providers=1'), { cache: 'no-store' }, 30_000).then(
      readProviders,
      () => {
        // Asked again next time: the machine may answer then.
        asked.delete(m);
        return [...BASE_PROVIDERS];
      },
    );
    asked.set(m, p);
  }
  return p;
};

/** Forgets what the machines said (a test, a connection just added). */
export const forgetImageProviders = () => asked.clear();

/** The generators of a machine: the base list at once, the machine's own when it answers. */
export const useImageProviders = (m: MachineId | undefined): readonly IImageProviderInfo[] => {
  const [list, setList] = useState<readonly IImageProviderInfo[]>(BASE_PROVIDERS);
  useEffect(() => {
    if (m === undefined) return;
    let live = true;
    void readImageProviders(m).then((got) => {
      if (live) setList(got);
    });
    return () => {
      live = false;
    };
  }, [m]);
  return list;
};

/** The models a generator offers for a job: for a change, only those that take a reference picture. */
export const modelsFor = (info: IImageProviderInfo | undefined, editing: boolean): IImageModel[] =>
  (info?.models ?? []).filter((m) => !editing || m.refs);

export interface IImagePrice {
  credits: number;
  usd: number;
}

export interface IPriceRequest {
  provider: ImageProvider;
  model?: string;
  count: number;
  shape?: string;
  /** For a change: the picture changed (the price of a model may depend on a reference). */
  source?: string;
}

export const estimateImages = async (
  m: MachineId,
  request: IPriceRequest,
  signal?: AbortSignal,
): Promise<IImagePrice> => {
  const body = await runtimeJson<unknown>(m, platformPath('/images?estimate=1'), {
    ...postJson({ ...request, prompt: 'price' }),
    signal,
  });
  if (!isRecord(body) || typeof body.credits !== 'number' || typeof body.usd !== 'number') {
    throw new Error('bad answer');
  }
  return { credits: body.credits, usd: body.usd };
};

export type TPrice =
  { k: 'none' } | { k: 'loading' } | { k: 'ready'; price: IImagePrice } | { k: 'failed'; why: string };

/** The price of a paid generator's job, asked again when what it depends on changes; 'none' for a free one. */
export const useImagePrice = (m: MachineId | undefined, request: IPriceRequest | null): TPrice => {
  const key = request && m !== undefined ? JSON.stringify(request) : '';
  const [state, setState] = useState<{ key: string; price: TPrice }>({ key: '', price: { k: 'none' } });
  useEffect(() => {
    if (key === '' || m === undefined) return;
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      estimateImages(m, JSON.parse(key) as IPriceRequest, ctl.signal).then(
        (price) => setState({ key, price: { k: 'ready', price } }),
        (failure: Error) => {
          if (!ctl.signal.aborted) setState({ key, price: { k: 'failed', why: failure.message } });
        },
      );
    }, 250);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
  }, [key, m]);
  if (key === '') return { k: 'none' };
  return state.key === key ? state.price : { k: 'loading' };
};

/** Credits as a person reads them: whole when whole, else up to two decimals. */
export const creditsText = (credits: number): string =>
  Number.isInteger(credits) ? String(credits) : String(Math.round(credits * 100) / 100);
