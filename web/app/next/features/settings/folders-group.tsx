// «Папки проектов» in the general settings: how many chats a folder shows, whether a closed folder still shows
// its live chats, whether the app suggests a folder for a chat. The person's own choices, kept as the settings' prefs.
import { Group, MenuRow, SwitchRow } from '@/next/features/settings/rows';
import { FOLDER_ROWS_CHOICES, setFolderPrefs, useFolderPrefs } from '@/next/features/sidebar/folder-prefs';
import { t } from '@/next/i18n';

const FoldersGroup = () => {
  const prefs = useFolderPrefs();
  // A value the person once had is never lost from the list: it stays among the choices.
  const rows = FOLDER_ROWS_CHOICES.includes(prefs.rows as (typeof FOLDER_ROWS_CHOICES)[number])
    ? [...FOLDER_ROWS_CHOICES]
    : [...FOLDER_ROWS_CHOICES, prefs.rows].sort((a, b) => a - b);
  return (
    <Group title={t('next.settings.general.folders')}>
      <MenuRow
        label={t('next.settings.general.folders.rows')}
        hint={t('next.settings.general.folders.rows.hint')}
        value={prefs.rows}
        choices={rows.map((n) => ({ value: n, label: String(n), sub: null }))}
        onPick={(value) => {
          if (typeof value === 'number') setFolderPrefs({ rows: value });
        }}
      />
      <SwitchRow
        label={t('next.settings.general.folders.peek')}
        hint={t('next.settings.general.folders.peek.hint')}
        on={prefs.peek}
        onChange={(peek) => setFolderPrefs({ peek })}
      />
      <SwitchRow
        label={t('next.settings.general.folders.suggest')}
        hint={t('next.settings.general.folders.suggest.hint')}
        on={prefs.suggest}
        onChange={(suggest) => setFolderPrefs({ suggest })}
      />
    </Group>
  );
};

export default FoldersGroup;
