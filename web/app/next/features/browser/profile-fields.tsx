import { t } from '@/next/i18n';
import type { IProfileChoice } from './profiles-api';
export const bp = (key: string, params?: Record<string, string | number>) =>
  t(`next.browserProfiles.${key}`, params);
/** The legacy common name is a label; explicit person-chosen names stay as entered. */
export const profileName = (profile: Pick<IProfileChoice, 'id' | 'name'>): string =>
  profile.id === 'common' && profile.name === 'Common' ? bp('common') : profile.name;
export function ProfileFields({
  name,
  color,
  onName,
  onColor,
  disabled = false,
}: {
  name: string;
  color: string;
  onName: (s: string) => void;
  onColor: (s: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="hx-browser-profiles__fields">
      <label className="hx-create__f">
        <span className="hx-create__l">{bp('name')}</span>
        <input
          className="hx-create__in"
          required
          value={name}
          onChange={(e) => onName(e.target.value)}
          disabled={disabled}
        />
      </label>
      <label className="hx-create__f">
        <span className="hx-create__l">{bp('color')}</span>
        <input
          type="color"
          value={/^#[0-9a-f]{6}$/i.test(color) ? color : '#6688cc'}
          onChange={(e) => onColor(e.target.value)}
          disabled={disabled}
        />
      </label>
    </div>
  );
}
export function ProfileOptions({ profiles }: { profiles: IProfileChoice[] }) {
  return profiles.map((p) => (
    <option key={p.id} value={p.id}>
      {profileName(p)}
    </option>
  ));
}
