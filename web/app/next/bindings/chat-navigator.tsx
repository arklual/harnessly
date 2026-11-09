// ⌘⌥J: jump through the open chat — its prompts and the subagents the agent started (beta «chatNav»). The search
// dialog draws it; the rows come from the chat's own stores, and a pick opens the chat at that entry as a search hit does.
import { useMemo, useState } from 'react';
import SearchDialog, { type ISearchRow } from '@/next/features/search/search-dialog';
import { filterGroups, navigatorGroups, sourceOf } from '@/next/features/navigator/rows';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import { revealMessage } from '@/next/stores/reveal';

interface IChatNavigator {
  open: boolean;
  onClose: () => void;
  conv: ConvId | null;
}

const clock = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });

const ChatNavigator = ({ open, onClose, conv }: IChatNavigator) => {
  const [query, setQuery] = useState('');
  // Read when the dialog opens: the chat does not move under a person who is choosing.
  const groups = useMemo(
    () =>
      open && conv
        ? navigatorGroups(
            sourceOf(conv),
            { prompts: t('next.navigator.prompts'), agents: t('next.navigator.agents') },
            clock,
          )
        : [],
    [open, conv],
  );
  const close = () => {
    setQuery('');
    onClose();
  };
  const pick = (row: ISearchRow) => {
    close();
    if (conv && row.target) revealMessage({ conv, entryId: row.target.entryId, turnId: row.target.turnId });
  };
  return (
    <SearchDialog
      open={open}
      onClose={close}
      query={query}
      onQuery={setQuery}
      scopes={[]}
      scope=""
      onScope={() => {}}
      results={filterGroups(groups, query)}
      recent={[]}
      idle={groups}
      words={{ title: t('next.navigator.title'), placeholder: t('next.navigator.placeholder') }}
      onPick={pick}
    />
  );
};

export default ChatNavigator;
