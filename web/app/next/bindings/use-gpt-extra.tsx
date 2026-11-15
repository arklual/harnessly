// «ChatGPT» in the picker of the start page: the plan's models in a tab beside the agents', the sign-in right
// there while nobody is signed in. A model picked starts the chat; words in the field are its first question.
import { lazy, Suspense, useEffect, useState } from 'react';
import { createChat, loadModels } from '@/next/features/chatgpt/api';
import { carryDraft, gptChatsChanged, useGptSignedIn } from '@/next/features/chatgpt/store';
import type { IPickerExtra } from '@/next/features/composer/model-picker';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';

const ChatGptSettings = lazy(() => import('@/next/features/chatgpt/settings-card'));

export const useGptExtra = (
  machine: MachineId | null,
  text: string,
  clear: () => void,
): IPickerExtra | null => {
  const signedIn = useGptSignedIn(machine);
  const [models, setModels] = useState<{ machine: MachineId; list: IPickerExtra['models'] } | null>(null);
  useEffect(() => {
    if (!machine || !signedIn) return undefined;
    let live = true;
    loadModels(machine).then(
      (list) => {
        if (live) setModels({ machine, list: list.map((m) => ({ id: m.slug, label: m.name || m.slug })) });
      },
      () => {
        if (live) setModels({ machine, list: [] });
      },
    );
    return () => {
      live = false;
    };
  }, [machine, signedIn]);
  if (!machine) return null;
  return {
    label: t('next.chatgpt.pick.label'),
    sub: t('next.chatgpt.pick.sub'),
    models: signedIn && models?.machine === machine ? models.list : null,
    signIn: signedIn ? null : (
      <Suspense fallback={null}>
        <ChatGptSettings machine={machine} />
      </Suspense>
    ),
    onPick: (model) => {
      const words = text;
      createChat(machine, model ?? '', '').then(
        (chat) => {
          carryDraft(chat.id, words, true);
          clear();
          gptChatsChanged();
          goTo({ kind: ROUTE.chatgpt, id: chat.id, machine });
        },
        () => goTo({ kind: ROUTE.settings, section: 'accounts' }),
      );
    },
  };
};
