// Shapes of what the runtime answers; the runtime itself is Go.

/** The release a runtime runs. */
export interface IRuntimeRelease {
  version: string;
  commit: string | null;
}
import type { IForecast } from '@/lib/hl-limits-forecast';
import type { ICodexbarPace, TRateLimitsSource } from '@/types/status';

export interface ILimitWindow {
  usedPct: number;
  /** Epoch seconds. */
  resetsAt: number;
  /**
   * Fields that are optional because the fleet gateway passes older runtimes' machines through as they are.
   * `sampledAt` is when the value was measured (epoch seconds), `pace` is codexbar's own window average.
   */
  sampledAt?: number | null;
  source?: TRateLimitsSource | null;
  forecast?: IForecast;
  pace?: ICodexbarPace | null;
}

export interface IExtraLimitWindow extends ILimitWindow {
  id: string;
  title: string;
}

export interface IProviderLimits {
  fiveHour: ILimitWindow | null;
  sevenDay: ILimitWindow | null;
  updatedAt: number | null;
  /** Subscription name from codexbar, e.g. «Claude Max 20x». */
  plan?: string | null;
  /** Last codexbar failure, shown next to the values it did not refresh. */
  error?: string | null;
  extra?: IExtraLimitWindow[];
}

/** What a runtime can do beyond plain conversations; the fleet gateway passes the field through as is. */
export interface IMachineFeatures {
  /** Subscription usage from codexbar, enabled by an absolute HL_CODEXBAR_BIN. */
  codexbar: boolean;
}

export type TMachineFeature = keyof IMachineFeatures;

export interface IMachine {
  id: string;
  name: string;
  host: string;
  user: string;
  cpuPct: number;
  memUsedPct: number;
  memTotalGb: number;
  uptimeSec: number;
  limits: { claude: IProviderLimits | null; codex: IProviderLimits | null };
  features: IMachineFeatures;
  /** Runtime release of this machine; absent on runtimes older runtimes and null on a dev checkout. */
  release?: IRuntimeRelease | null;
}
