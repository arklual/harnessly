// A conversation with ChatGPT: the feed and the field of a chat, without an agent — no terminal, no tools,
// no files of the machine. The question goes to the machine, which asks ChatGPT on the person's plan and streams the
// answer back; the model and the depth of thinking are the plan's own.
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import Composer from '@/next/features/composer/composer';
import type { ComposerAttachment, ComposerMode } from '@/next/features/composer/types';
import CopyButton from '@/next/features/chat/copy-button';
import {
  fileUrl,
  followAnswer,
  deleteChat,
  loadChat,
  loadModels,
  loadState,
  refusalCode,
  renameChat,
  sendMessage,
  stopAnswer,
} from '@/next/features/chatgpt/api';
import GptChatMenu from '@/next/features/chatgpt/chat-menu';
import ModelChip from '@/next/features/chatgpt/model-chip';
import {
  GPT_FILE_TYPES,
  GPT_IMAGE_TYPES,
  GPT_STATUS,
  refusalKey,
  USAGE_URL,
  type IGptChat,
  type IGptMessage,
  type IGptModel,
  type IGptUpload,
} from '@/next/features/chatgpt/model';
import { gptChatsChanged, takeDraft } from '@/next/features/chatgpt/store';
import { t } from '@/next/i18n';
import { goTo, HOME } from '@/next/routes/routes';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/chatgpt.css';

// The answer's Markdown, loaded as the chat's own is (md.tsx), without its pictures from the machine's paths: this chat
// has no access to the machine's files.
const Markdown = lazy(() => import('@/next/features/chat/markdown'));

const MAX_TEXT = 200_000;
const FILE_MAX = 20 << 20;
const FILES_MAX = 8;
const NOTE_EXIT_MS = 180;

interface IPicked extends ComposerAttachment {
  upload: IGptUpload | null;
}

const readBase64 = (file: File): Promise<string> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      resolve(url.slice(url.indexOf(',') + 1));
    };
    reader.onerror = () => reject(reader.error ?? new Error('read'));
    reader.readAsDataURL(file);
  });

/** Why a file cannot go, or null. */
const fileProblem = (file: File, model: IGptModel | null): string | null => {
  const image = GPT_IMAGE_TYPES.includes(file.type);
  if (!image && !GPT_FILE_TYPES.includes(file.type)) return t('next.chatgpt.file.type');
  if (file.size > FILE_MAX) return t('next.chatgpt.file.size');
  if (model && image && !model.images) return t('next.chatgpt.file.noImages');
  return null;
};

const Failure = ({ code }: { code: string }) => (
  <div className="hx-gpt__fail" role="status">
    <Icon name="circle-alert" size="sm" />
    <span>{t(refusalKey(code))}</span>
    {code === 'usage-limit' && (
      <a href={USAGE_URL} target="_blank" rel="noreferrer noopener">
        {t('next.chatgpt.usageLink')}
      </a>
    )}
  </div>
);

const Files = ({ machine, chat, message }: { machine: MachineId; chat: string; message: IGptMessage }) => (
  <div className="hx-gpt__files">
    {message.files?.map((f) =>
      f.mime.startsWith('image/') ? (
        <a
          key={f.id}
          className="hx-gpt__pic"
          href={fileUrl(machine, chat, f.id)}
          target="_blank"
          rel="noreferrer"
        >
          <img src={fileUrl(machine, chat, f.id)} alt={f.name} loading="lazy" />
        </a>
      ) : (
        <a key={f.id} className="hx-gpt__doc" href={fileUrl(machine, chat, f.id)} download={f.name}>
          <Icon name="file-text" size="sm" />
          <span>{f.name}</span>
        </a>
      ),
    )}
  </div>
);

const Question = ({ machine, chat, message }: { machine: MachineId; chat: string; message: IGptMessage }) => (
  <div className="hx-msg" data-kind="user">
    <div className="hx-msg-user" data-send="sent">
      {(message.files?.length ?? 0) > 0 && <Files machine={machine} chat={chat} message={message} />}
      {message.text && (
        <div className="hx-bubble">
          {message.text.split('\n').map((line, i) => (
            <p key={i}>{line}</p>
          ))}
        </div>
      )}
      {message.text && (
        <div className="hx-turnacts hx-turnacts--user">
          <CopyButton text={message.text} />
        </div>
      )}
    </div>
  </div>
);

const Answer = ({ text, message, live }: { text: string; message: IGptMessage | null; live: boolean }) => (
  <div className="hx-msg hx-gpt__answer" data-kind="turn" data-live={live || undefined}>
    {text ? (
      <Suspense fallback={<div className="hx-md">{text}</div>}>
        <Markdown md={text} />
      </Suspense>
    ) : (
      live && <span className="hx-gpt__dots" aria-label={t('next.chatgpt.thinking')} />
    )}
    {message?.status === GPT_STATUS.stopped && (
      <div className="hx-gpt__note">{t('next.chatgpt.stopped')}</div>
    )}
    {message?.status === GPT_STATUS.failed && <Failure code={message.error?.code ?? ''} />}
    {!live && text && (
      <div className="hx-turnacts">
        <CopyButton text={text} />
      </div>
    )}
  </div>
);

interface IChatScreenProps {
  machine: MachineId;
  id: string;
  onOpenSettings: () => void;
  onOpenDrawer: () => void;
}

type TLoad = { k: 'loading' } | { k: 'failed'; reason: string } | { k: 'ready'; chat: IGptChat };

const ChatScreen = ({ machine, id, onOpenSettings, onOpenDrawer }: IChatScreenProps) => {
  const [load, setLoad] = useState<TLoad>({ k: 'loading' });
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [models, setModels] = useState<readonly IGptModel[]>([]);
  // The person's pick of model and depth in this screen; null: the chat's own, else the plan's first.
  const [pick, setPick] = useState<{ model: string; effort: string } | null>(null);
  const [text, setText] = useState(() => takeDraft(id));
  const [picked, setPicked] = useState<readonly IPicked[]>([]);
  const [running, setRunning] = useState(false);
  const [live, setLive] = useState('');
  // The last refusal of a send; open while it stands, kept through its exit.
  const [failure, setFailure] = useState({ code: '', open: false });
  const feedRef = useRef<HTMLDivElement>(null);
  const unfollow = useRef<(() => void) | null>(null);
  const errorFlag = usePresentFlag(failure.open, NOTE_EXIT_MS);

  const read = useCallback(
    () =>
      loadChat(machine, id).then(
        (answer) => {
          setLoad({ k: 'ready', chat: answer.chat });
          return answer;
        },
        (failure: unknown) => {
          setLoad({ k: 'failed', reason: t(refusalKey(refusalCode(failure))) });
          return null;
        },
      ),
    [machine, id],
  );

  const follow = useCallback(() => {
    unfollow.current?.();
    setRunning(true);
    setLive('');
    unfollow.current = followAnswer(machine, id, {
      onDelta: (delta) => setLive((was) => was + delta),
      onDone: () => {
        unfollow.current = null;
        void read().then(() => {
          setRunning(false);
          setLive('');
          gptChatsChanged();
        });
      },
    });
  }, [machine, id, read]);

  useEffect(() => {
    let live = true;
    void read().then((answer) => {
      if (live && answer?.running) follow();
    });
    loadState(machine).then(
      (state) => {
        if (live) setSignedIn((state.accounts ?? []).some((a) => a.signedIn && a.planUse));
      },
      () => {
        if (live) setSignedIn(false);
      },
    );
    loadModels(machine).then(
      (list) => {
        if (live) setModels(list);
      },
      () => undefined,
    );
    return () => {
      live = false;
      unfollow.current?.();
      unfollow.current = null;
    };
  }, [machine, read, follow]);

  // The picked model, else the chat's, else the first of the plan's; the depth goes with it.
  const kept = load.k === 'ready' ? load.chat : null;
  const wanted = pick?.model ?? kept?.model ?? '';
  const chosen = models.find((m) => m.slug === wanted) ?? models[0] ?? null;
  let effort = chosen?.defaultEffort ?? '';
  if (pick && pick.model === chosen?.slug) effort = pick.effort;
  else if (!pick && kept?.effort && kept.model === chosen?.slug) effort = kept.effort;

  const messages = load.k === 'ready' ? (load.chat.messages ?? []) : [];
  // The feed follows the answer as it grows, unless the person scrolled up.
  useEffect(() => {
    const el = feedRef.current;
    if (!el) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 160) el.scrollTop = el.scrollHeight;
  }, [messages.length, live]);

  const onFiles = (files: File[]) => {
    const room = Math.max(0, FILES_MAX - picked.length);
    const next = files.slice(0, room).map((file): IPicked => {
      const problem = fileProblem(file, chosen);
      return {
        id: `${file.name}-${file.size}-${Math.random().toString(36).slice(2, 8)}`,
        name: file.name,
        kind: GPT_IMAGE_TYPES.includes(file.type) ? 'image' : 'file',
        status: problem ? 'error' : 'uploading',
        error: problem,
        upload: null,
      };
    });
    setPicked((was) => [...was, ...next]);
    next.forEach((item, i) => {
      if (item.error) return;
      const file = files[i];
      readBase64(file).then(
        (data) =>
          setPicked((was) =>
            was.map((p) =>
              p.id === item.id
                ? { ...p, status: 'ready', upload: { name: file.name, mime: file.type, data } }
                : p,
            ),
          ),
        () =>
          setPicked((was) =>
            was.map((p) =>
              p.id === item.id ? { ...p, status: 'error', error: t('next.chatgpt.file.read') } : p,
            ),
          ),
      );
    });
  };

  const submit = () => {
    const words = text.trim();
    const uploads = picked.filter((p) => p.status === 'ready' && p.upload).map((p) => p.upload as IGptUpload);
    if (running || !chosen || (!words && uploads.length === 0)) return;
    if (picked.some((p) => p.status === 'uploading')) return;
    setFailure((f) => ({ ...f, open: false }));
    const was = { text, picked };
    setText('');
    setPicked([]);
    sendMessage(machine, id, { text: words, model: chosen.slug, effort, files: uploads }).then(
      (answer) => {
        setLoad((l) =>
          l.k === 'ready'
            ? { k: 'ready', chat: { ...l.chat, messages: [...(l.chat.messages ?? []), answer.message] } }
            : l,
        );
        gptChatsChanged();
        follow();
      },
      (refused: unknown) => {
        // Nothing was kept: the words and the files come back to the field.
        setText(was.text);
        setPicked(was.picked);
        setFailure({ code: refusalCode(refused) || 'other', open: true });
      },
    );
  };

  let mode: ComposerMode = 'idle';
  if (running) mode = 'working';
  else if (messages.length === 0) mode = 'new';

  return (
    <div className="hx-gpt">
      <div className="hx-gpt__bar">
        <button
          type="button"
          className="hx-head-btn hx-gpt__back"
          aria-label={t('next.chatgpt.chats')}
          onClick={onOpenDrawer}
        >
          <Icon name="chevron-left" />
        </button>
        <span className="hx-gpt__mark" aria-hidden="true">
          <i className="hx-agent" data-agent="codex" />
        </span>
        <span className="hx-gpt__title">
          {load.k === 'ready' && load.chat.title ? load.chat.title : t('next.chatgpt.newChat')}
        </span>
        {load.k === 'ready' && (
          <GptChatMenu
            title={load.chat.title}
            onRename={(title) =>
              renameChat(machine, id, title).then(() => {
                gptChatsChanged();
                void read();
              })
            }
            onDelete={() =>
              deleteChat(machine, id).then(() => {
                gptChatsChanged();
                goTo(HOME);
              })
            }
          />
        )}
      </div>
      <div className="hx-chat">
        <div className="hx-feed" ref={feedRef}>
          <div className="hx-feed__inner">
            {load.k === 'failed' && (
              <LoadFailed
                size={LOAD_FAILED_SIZE.page}
                title={t('next.chatgpt.loadFailed')}
                reason={load.reason}
                onRetry={() => void read()}
              />
            )}
            {load.k === 'ready' && (
              <div className="hx-thread">
                {messages.length === 0 && !running && (
                  <div className="hx-gpt__empty">
                    <b>{t('next.chatgpt.emptyTitle')}</b>
                    <span>{t('next.chatgpt.emptyNote')}</span>
                  </div>
                )}
                {messages.map((m) =>
                  m.role === 'user' ? (
                    <Question key={m.id} machine={machine} chat={id} message={m} />
                  ) : (
                    <Answer key={m.id} text={m.text} message={m} live={false} />
                  ),
                )}
                {running && <Answer text={live} message={null} live />}
              </div>
            )}
          </div>
        </div>
        <div className="hx-dock" data-screen>
          {signedIn === false && (
            <div className="hx-gpt__signin">
              <span>{t('next.chatgpt.signInFirst')}</span>
              <button type="button" className="hx-set__btn" onClick={onOpenSettings}>
                {t('next.chatgpt.toSettings')}
              </button>
            </div>
          )}
          {errorFlag.mounted && (
            <div className="hx-gpt__error" data-state={errorFlag.state}>
              <Failure code={failure.code} />
            </div>
          )}
          <Composer
            value={text}
            onChange={setText}
            onSubmit={submit}
            mode={mode}
            placeholder={t('next.chatgpt.placeholder')}
            onStop={() => stopAnswer(machine, id)}
            picker={null}
            tools={
              <ModelChip
                models={models}
                model={chosen}
                effort={effort}
                onModel={(m) => setPick({ model: m.slug, effort: m.defaultEffort ?? '' })}
                onEffort={(e) => {
                  if (chosen) setPick({ model: chosen.slug, effort: e });
                }}
              />
            }
            attachments={[...picked]}
            onFiles={onFiles}
            onRemoveAttachment={(rid) => setPicked((was) => was.filter((p) => p.id !== rid))}
            onRetryAttachment={(rid) => setPicked((was) => was.filter((p) => p.id !== rid))}
            recording={null}
            onCancelRecording={() => undefined}
            onFinishRecording={() => undefined}
            maxText={MAX_TEXT}
            quiet={load.k !== 'ready' || signedIn === false}
          />
          {messages.length === 0 && <div className="hx-gpt__plan">{t('next.chatgpt.planNote')}</div>}
        </div>
      </div>
    </div>
  );
};

export default ChatScreen;
