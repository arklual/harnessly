// The pages the shell opens by address, each in its own chunk. Imported directly they all came with the
// first frame: Sinopia Design, the review with CodeMirror, the settings and the apps' pages were 2.2 of the 3.7 MB of
// the shell's chunk an iPhone parses before it draws the chat. Now a page comes the first time it is opened, with a
// skeleton while it does, and a chunk that does not come says so with a retry (LazyView).
import type { ComponentType, JSX } from 'react';
import LazyView, { preloadView } from '@/next/ui/lazy-view';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import { t } from '@/next/i18n';
import type * as AppMainPageModule from '@/next/bindings/app-main';
import type * as ReviewAppPageModule from '@/next/bindings/review-app';
import type * as AppAboutPageModule from '@/next/bindings/app-page';
import type * as AppDocPageModule from '@/next/bindings/app-doc';
import type * as AppsListPageModule from '@/next/bindings/apps-list';
import type * as HelpPageModule from '@/next/bindings/help';
import type * as AppsScreenModule from '@/next/bindings/apps';
import type * as SettingsScreenModule from '@/next/bindings/settings';
import type * as DesignlyViewModule from '@/next/bindings/designly';
import type * as DesignlySiteModule from '@/next/bindings/designly-site';
import type * as KitPageModule from '@/next/bindings/kit-page';
import type * as InviteAcceptModule from '@/next/features/designly/access/invite-accept';

const FALLBACK = (
  <Delayed>
    <Skeleton className="hx-lscreen__sk" />
  </Delayed>
);

// A page's type names its module (tsc -b writes declarations, and the pages' props interfaces are not exported).
type Later<M extends { default: (props: never) => unknown }> = (
  props: Parameters<M['default']>[0],
) => JSX.Element;

const loadSettings = () => import('@/next/bindings/settings');
const loadDesignly = () => import('@/next/bindings/designly');
const loadDesignlySite = () => import('@/next/bindings/designly-site');
// The load is made once per page, outside the component: LazyView keeps its lazy component per this function.
const later =
  <P extends object>(load: () => Promise<{ default: ComponentType<P> }>) =>
  (props: P) => (
    <LazyView load={load} props={props} fallback={FALLBACK} title={t('next.load.screen')} size="page" />
  );

export const AppMainPage: Later<typeof AppMainPageModule> = later(() => import('@/next/bindings/app-main'));
export const ReviewAppPage: Later<typeof ReviewAppPageModule> = later(
  () => import('@/next/bindings/review-app'),
);
export const AppAboutPage: Later<typeof AppAboutPageModule> = later(() => import('@/next/bindings/app-page'));
export const AppDocPage: Later<typeof AppDocPageModule> = later(() => import('@/next/bindings/app-doc'));
export const AppsListPage: Later<typeof AppsListPageModule> = later(
  () => import('@/next/bindings/apps-list'),
);
export const HelpPage: Later<typeof HelpPageModule> = later(() => import('@/next/bindings/help'));
export const AppsScreen: Later<typeof AppsScreenModule> = later(() => import('@/next/bindings/apps'));
export const SettingsScreen: Later<typeof SettingsScreenModule> = later(loadSettings);
export const DesignlyView: Later<typeof DesignlyViewModule> = later(loadDesignly);
export const DesignlySite: Later<typeof DesignlySiteModule> = later(loadDesignlySite);
export const KitPage: Later<typeof KitPageModule> = later(() => import('@/next/bindings/kit-page'));
export const InviteAccept: Later<typeof InviteAcceptModule> = later(
  () => import('@/next/features/designly/access/invite-accept'),
);

/** The pages fetched ahead of their first opening (bindings/shell.tsx): Settings and Sinopia Design, the two the sidebar
 *  opens most. */
const WARM = {
  settings: () => preloadView(loadSettings),
  designly: () => preloadView(loadDesignly),
  site: () => preloadView(loadDesignlySite),
} as const;
export type TWarmPage = keyof typeof WARM;
export const isWarmPage = (name: string | undefined): name is TWarmPage => name !== undefined && name in WARM;

/** Fetches a page's chunk now; a failure is left to the page's own opening, which says it with a retry. */
export const warmPage = (name: TWarmPage): void => {
  WARM[name]().catch(() => undefined);
};
