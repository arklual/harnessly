import { TwDialog, TwKbd } from '@/components/agents/ui';
import { useModKey } from '@/components/agents/sidebar/organizer-actions';
import { t } from '@/lib/i18n';

const SHORTCUTS = [
  { keys: ['K'], label: t('sidebar.shortcutsDialog.search') },
  { keys: ['⇧', 'O'], label: t('sidebar.shortcutsDialog.newConversation') },
  { keys: ['1…9'], label: t('sidebar.shortcutsDialog.openPinned') },
  { keys: ['B'], label: t('sidebar.shortcutsDialog.togglePanel') },
  { keys: ['.'], label: t('sidebar.shortcutsDialog.rightPanel') },
  { keys: ['J'], label: t('sidebar.shortcutsDialog.findInConversation') },
] as const;

const ShortcutsDialog = ({ open, onClose }: { open: boolean; onClose: () => void }) => {
  const modKey = useModKey();
  return (
    <TwDialog open={open} onClose={onClose} title={t('sidebar.shortcutsDialog.title')}>
      <dl className="tw-shortcuts">
        {SHORTCUTS.map((shortcut) => (
          <div key={shortcut.label} className="tw-shortcuts-row">
            <dt>{shortcut.label}</dt>
            <dd>
              <TwKbd>
                {modKey}
                {shortcut.keys.join('')}
              </TwKbd>
            </dd>
          </div>
        ))}
      </dl>
    </TwDialog>
  );
};

export default ShortcutsDialog;
