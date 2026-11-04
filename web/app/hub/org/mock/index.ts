// The typed mock layer: what the screens run on until the live handles exist. It is the only place with made-up
// numbers; every section adds its own file here and is mixed in below.
import type { OrgApi } from '../api';
import { onboardingMock } from './onboarding';
import { memberMock } from './member';
import { overviewMock } from './overview';
import { accountsMock } from './accounts';
import { analyticsMock } from './analytics';
import { envMock } from './env';
import { machinesMock } from './machines';
import { shellMock } from './shell';
import { teamsMock } from './teams';
import { tenantMock } from './tenant';
import type { MockCtx } from './ctx';
import { setShownCurrency } from '../money';

// The mocks are made in rubles at 90 a dollar (overview.ts).
setShownCurrency('RUB', 90);

export const mockOrgApi = (ctx: MockCtx): OrgApi => ({
  ...shellMock(ctx),
  ...onboardingMock(),
  ...overviewMock(ctx),
  ...memberMock(ctx),
  ...analyticsMock(ctx),
  ...machinesMock(ctx),
  ...teamsMock(ctx),
  ...tenantMock(ctx),
  ...envMock(ctx),
  ...accountsMock(ctx),
});
