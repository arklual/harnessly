import { OrgApiError } from '../api/errors';
import type { LooseSpace, OnboardingApi } from '../api/onboarding';
import { later } from './ctx';

const SPACES: LooseSpace[] = [
  { id: 'sp-1', slug: 'product', name: 'Продукт', owner: true },
  { id: 'sp-2', slug: 'platform', name: 'Платформа', owner: true },
  { id: 'sp-3', slug: 'friends', name: 'Общее с друзьями', owner: false },
];

export const onboardingMock = (): OnboardingApi => ({
  looseSpaces: () => later(SPACES),
  createOrg: async ({ name, spaceIds }) => {
    if (name.trim() === '' || name.trim().length > 60) throw new OrgApiError('invalid_request', 'name 1-60');
    if (spaceIds.some((id) => !SPACES.some((s) => s.id === id && s.owner))) {
      throw new OrgApiError('forbidden', 'not an owner');
    }
    return later({ slug: 'acme' }, 700);
  },
});
