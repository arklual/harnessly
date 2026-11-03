// One typed surface for every screen of the organization hub. `createOrgApi` picks mocks or live calls by the single
// setting in ../mode.ts; screens only see `OrgApi`.
import type { DataMode } from '../mode';
import type { Role } from '../types';
import { liveOrgApi } from './live';
import { mockOrgApi } from '../mock';
import type { AnalyticsApi } from './analytics';
import type { MachinesApi } from './machines';
import type { MemberApi } from './member';
import type { OnboardingApi } from './onboarding';
import type { OverviewApi } from './overview';
import type { ShellApi } from './shell';
import type { TeamsApi } from './teams';
import type { TenantApi } from './tenant';

export type OrgApi = ShellApi &
  OverviewApi &
  AnalyticsApi &
  MachinesApi &
  TeamsApi &
  MemberApi &
  OnboardingApi &
  TenantApi;

export const createOrgApi = (mode: DataMode, role: Role): OrgApi =>
  import.meta.env.DEV && mode === 'mock' ? mockOrgApi({ role }) : liveOrgApi();
