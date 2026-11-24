// Whose designs the library shows: «Все» is every card, «Мои» the ones the person made, «Общие» the ones teammates
// made. A card of an older project has no owner and is only in «Все»: it is never guessed to be anyone's.
import { locale, t } from '@/next/i18n';
import type { DesignAccessSource, DesignArtifact, DesignOwner } from '@/next/protocol/v2';

export const DESIGN_SCOPE = { all: 'all', mine: 'mine', shared: 'shared' } as const;
export type TDesignScope = (typeof DESIGN_SCOPE)[keyof typeof DESIGN_SCOPE];
export const DESIGN_SCOPES = [DESIGN_SCOPE.all, DESIGN_SCOPE.mine, DESIGN_SCOPE.shared] as const;

export const inScope = (
  scope: TDesignScope,
  owner: DesignOwner | null,
  source?: DesignAccessSource,
): boolean => {
  if (scope === DESIGN_SCOPE.all) return true;
  if (source) return scope === DESIGN_SCOPE.mine ? source === 'owner' : source !== 'owner';
  if (owner === null) return false;
  return scope === DESIGN_SCOPE.mine ? owner.me : !owner.me;
};

/** The first letter of the name, for the avatar. */
export const initialOf = (name: string): string => {
  const [first] = Array.from(name.trim());
  return first ? first.toUpperCase() : '';
};

export const designAccessLabel = ({
  role,
  accessSource,
  accessUntil,
}: Pick<DesignArtifact, 'role' | 'accessSource' | 'accessUntil'>): string => {
  const parts: string[] = [];
  if (accessSource && accessSource !== 'owner') parts.push(t(`next.designly.access.${accessSource}`));
  if (role) parts.push(t(`next.access.role.${role}`));
  else if (accessSource === 'owner') parts.push(t('next.access.role.owner'));
  const time = accessUntil && Date.parse(accessUntil);
  if (typeof time === 'number' && Number.isFinite(time)) {
    const when = new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'short' }).format(time);
    parts.push(t('next.designly.access.until', { when }));
  }
  return parts.join(' · ');
};
