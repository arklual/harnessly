// The chat's own field for the galleries: the composer with nothing behind it, as the island and the cards
// over a screen show it.
import { useState } from 'react';
import Composer from '@/next/features/composer/composer';

const nothing = () => {};

export const DemoIslandField = ({
  placeholder = 'Опишите правку или выделите блок',
}: {
  placeholder?: string;
}) => {
  const [text, setText] = useState('');
  return (
    <div className="hx-dock" data-screen>
      <Composer
        value={text}
        onChange={setText}
        onSubmit={() => setText('')}
        mode="idle"
        placeholder={placeholder}
        onStop={() => Promise.resolve()}
        picker={null}
        attachments={[]}
        onFiles={nothing}
        onRemoveAttachment={nothing}
        onRetryAttachment={nothing}
        recording={null}
        onCancelRecording={nothing}
        onFinishRecording={nothing}
        maxText={100000}
      />
    </div>
  );
};
