import { createContext, type ReactNode, useContext, useMemo } from 'react';

import { Button, EmptyState, Spinner } from '@kit';

import { createOrgApi, type OrgApi } from './api';
import { orgErrorText } from './api/errors';
import { dataMode, mockRole } from './mode';
import type { Scope, Section, ShellData, TenantRef } from './types';
import { ALL_SCOPE } from './types';
import { useShellLoad } from './use-shell';

export interface OrgCtx {
  api: OrgApi;
  shell: ShellData;
  scope: Scope;
  /** The path prefix of the current scope: `/o/acme/product`. */
  base: string;
  section: Section;
  /** Loads the shell again behind the screen: after the tenant got a new name. */
  refreshShell: () => void;
}

const Ctx = createContext<OrgCtx | null>(null);

export const useOrg = (): OrgCtx => {
  const v = useContext(Ctx);
  if (!v) throw new Error('OrgProvider is missing');
  return v;
};

export const scopeOf = (shell: ShellData, scopeSlug: string): Scope | null => {
  if (scopeSlug === ALL_SCOPE) return { kind: 'org' };
  const tenant: TenantRef | undefined = shell.tenants.find((t) => t.slug === scopeSlug);
  return tenant ? { kind: 'tenant', tenant } : null;
};

/** Loads the shell data once per organization and scope, then hands the screens the api and the scope. */
export const OrgProvider = ({
  org,
  scopeSlug,
  section,
  children,
}: {
  org: string;
  scopeSlug: string;
  section: Section;
  children: (scope: Scope) => ReactNode;
}) => {
  const api = useMemo(() => createOrgApi(dataMode(), mockRole()), []);
  const { data, error, reload, refresh } = useShellLoad(api, org, scopeSlug);
  if (error) {
    return (
      <div className="ho-state">
        <EmptyState title="Хаб не открылся" actions={<Button onClick={reload}>Повторить</Button>}>
          {orgErrorText(error)}
        </EmptyState>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="ho-state" role="status" aria-label="Загрузка">
        <Spinner size="m" />
      </div>
    );
  }
  const scope = scopeOf(data, scopeSlug);
  if (!scope) {
    return (
      <div className="ho-state">
        <EmptyState title="Такого тенанта нет">Выберите другой в переключателе слева.</EmptyState>
      </div>
    );
  }
  return (
    <Inner
      api={api}
      shell={data}
      scope={scope}
      scopeSlug={scopeSlug}
      org={org}
      section={section}
      refreshShell={refresh}
    >
      {children}
    </Inner>
  );
};

const Inner = ({
  api,
  shell,
  scope,
  scopeSlug,
  org,
  section,
  refreshShell,
  children,
}: {
  api: OrgApi;
  shell: ShellData;
  scope: Scope;
  scopeSlug: string;
  org: string;
  section: Section;
  refreshShell: () => void;
  children: (scope: Scope) => ReactNode;
}) => {
  const value = useMemo<OrgCtx>(
    () => ({ api, shell, scope, base: `/o/${org}/${scopeSlug}`, section, refreshShell }),
    [api, shell, scope, org, scopeSlug, section, refreshShell],
  );
  return <Ctx.Provider value={value}>{children(scope)}</Ctx.Provider>;
};
