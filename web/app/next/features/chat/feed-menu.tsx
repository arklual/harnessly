// The feed's own menu (a right click or a two-finger tap in the chat): the answer under the pointer copied or
// branched from, the chat's link, the right pane. Selected text, links, pictures and code fields keep the browser's
// menu (ui/context-menu.tsx); a held finger selects text here, so it opens nothing.
import { useKeyHint } from '@/next/features/keys/keys-help';
import { runKeyAction } from '@/next/features/keys/use-keys';
import type { FeedItem } from '@/next/features/chat/types';
import { MenuGap, MenuItem, PointMenu, type TContextMenu } from '@/next/ui/context-menu';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

type TTurnItem = Extract<FeedItem, { kind: 'turn' }>;

/** The finished turn with an answer the menu was asked on, if any. */
const turnAt = (target: Element | null, items: readonly FeedItem[]): TTurnItem | null => {
  const key = target?.closest<HTMLElement>('.hx-turnwrap[data-key]')?.dataset.key;
  if (key === undefined) return null;
  const item = items.find((i) => i.key === key);
  return item?.kind === 'turn' && item.answer !== null && item.turn.status === 'done' ? item : null;
};

const copyAnswer = (md: string) =>
  void navigator.clipboard.writeText(md).then(
    () => toast(t('next.keys.copied')),
    () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
  );

interface IFeedMenuProps {
  menu: TContextMenu;
  items: readonly FeedItem[];
  /** The latest answer: what the menu copies away from any turn, as ⌘⌥C does. */
  lastAnswer: string | null;
  onFork?: (turnId: string) => Promise<unknown>;
  onCopyLink?: () => void;
}

const FeedMenu = ({ menu, items, lastAnswer, onFork, onCopyLink }: IFeedMenuProps) => {
  const copyKey = useKeyHint('global.copyAnswer');
  const paneKey = useKeyHint('global.pane');
  const turn = turnAt(menu.target, items);
  const answer = turn?.answer?.md ?? lastAnswer;
  return (
    <PointMenu menu={menu} label={t('next.feedMenu.label')} kind="feed">
      {answer !== null && (
        <MenuItem
          icon="copy"
          hint={answer === lastAnswer ? copyKey : null}
          onClick={() => copyAnswer(answer)}
        >
          {t('next.feedMenu.copyAnswer')}
        </MenuItem>
      )}
      {turn && onFork && (
        <MenuItem icon="git-fork" onClick={() => void onFork(turn.turn.id)}>
          {t('next.handoff.forkHere')}
        </MenuItem>
      )}
      {answer !== null && <MenuGap />}
      {onCopyLink && (
        <MenuItem icon="link" onClick={onCopyLink}>
          {t('next.feedMenu.copyChatLink')}
        </MenuItem>
      )}
      <MenuItem icon="panel-right" hint={paneKey} onClick={() => runKeyAction('global.pane')}>
        {t('next.feedMenu.pane')}
      </MenuItem>
    </PointMenu>
  );
};

export default FeedMenu;
