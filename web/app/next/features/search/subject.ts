// What a row of the search (⌘K) stands for, as the side of the search asks about it.
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { ICatalogDoc, ICatalogSetting, IFound } from '@/next/features/search/sources-api';
import type { DesignArtifact } from '@/next/protocol/v2';

export const SUBJECT = {
  chat: 'chat',
  design: 'design',
  doc: 'doc',
  setting: 'setting',
  found: 'found',
  app: 'app',
} as const;

export type TSearchSubject =
  | { k: typeof SUBJECT.chat; id: string }
  /** `board` is the board the row is of; null for the design itself. */
  | { k: typeof SUBJECT.design; design: DesignArtifact; board: string | null }
  | { k: typeof SUBJECT.doc; doc: ICatalogDoc }
  | { k: typeof SUBJECT.setting; setting: ICatalogSetting }
  /** `conn` is the connection that found it. */
  | { k: typeof SUBJECT.found; conn: string; found: IFound }
  | { k: typeof SUBJECT.app; app: IInstalledApp };
