// How the cards of the chat look (Settings → General, «Что показывать в ленте»): the block «Сделано за ход» at the end
// of an answer, the previews of documents and designs, the screenshots large. Kept with the kinds of the feed
// (stores/feed-kinds.ts), drawn under their switches.
import type { TShownKind, TShownKinds } from '@/next/features/chat/outputs';
import { SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

export const FEED_LOOK_ROWS = ['made', 'preview', 'bigShots'] as const satisfies readonly TShownKind[];

interface IFeedLookRowsProps {
  feedKinds: TShownKinds;
  onFeedKind: (kind: TShownKind, on: boolean) => void;
}

const FeedLookRows = ({ feedKinds, onFeedKind }: IFeedLookRowsProps) =>
  FEED_LOOK_ROWS.map((kind) => (
    <SwitchRow
      key={kind}
      label={t(`next.settings.general.feed.${kind}`)}
      hint={t(`next.settings.general.feed.${kind}.hint`)}
      on={feedKinds[kind]}
      onChange={(on) => onFeedKind(kind, on)}
    />
  ));

export default FeedLookRows;
