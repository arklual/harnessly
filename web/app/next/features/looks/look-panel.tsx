// A scope's own look: off (it wears what it inherits: the folder's or the one for all, or the app as always), a
// ready look, or one of the person's own made in the editor. Settings → Оформление shows it for all chats, the sheet
// of a folder or a chat for that one (look-sheet.tsx).
import LookEditor, { PresetGallery } from '@/next/features/looks/look-editor';
import { putLook, useLooks } from '@/next/features/looks/store';
import { STARTER_LOOK, scopeLook, type ILook, type TLookScope } from '@/next/features/looks/types';
import { Group, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

const LookPanel = ({ scope }: { scope: TLookScope }) => {
  const looks = useLooks();
  const own = scopeLook(looks, scope);
  const put = (look: ILook | null) => putLook(scope, look);
  return (
    <Group title={t(`next.looks.title.${scope.k}`)} card="hx-lk-card" foot={t(`next.looks.foot.${scope.k}`)}>
      <SwitchRow
        label={t('next.looks.on')}
        hint={own ? undefined : t(`next.looks.off.${scope.k}`)}
        on={own !== null}
        onChange={(on) => put(on ? (looks.all && scope.k !== 'all' ? looks.all : STARTER_LOOK) : null)}
      />
      {own && <PresetGallery current={own} onPick={put} />}
      {own && <LookEditor look={own} onChange={put} />}
    </Group>
  );
};

export default LookPanel;
