// A chat whose machine the page does not know yet (a cold load of its address: no cached feed, no list): the feed's
// skeleton over a quiet field. The composer is there from the first frame, so the page does not jump when the chat comes.
import Composer from '@/next/features/composer/composer';
import Feed from '@/next/features/chat/feed';
import { useState } from 'react';

const none = (): void => {};
const done = (): Promise<void> => Promise.resolve();
/** The field is quiet, so it takes no text; the number only has to be above the empty value's length. */
const NO_TEXT_LIMIT = 1;

const QuietChat = () => {
  const [openedAt] = useState(() => Date.now());
  return (
    <Feed
      items={null}
      hasOlder={false}
      loadingOlder={false}
      onLoadOlder={none}
      density="compact"
      offline={false}
      enterAfter={openedAt}
      machineName=""
      onContinueTurn={done}
      onRetryTurn={done}
      onRetryMessage={done}
      onDeleteMessage={done}
      dock={
        <Composer
          quiet
          value=""
          onChange={none}
          onSubmit={none}
          mode="idle"
          onStop={done}
          picker={null}
          attachments={[]}
          onFiles={none}
          onRemoveAttachment={none}
          onRetryAttachment={none}
          recording={null}
          onCancelRecording={none}
          onFinishRecording={none}
          maxText={NO_TEXT_LIMIT}
        />
      }
    />
  );
};

export default QuietChat;
