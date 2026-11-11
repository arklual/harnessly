// What an app or an agent asks the person through the smart notice's open slot, drawn in the black plate:
// a status, a progress, a choice, a confirmation, a one-line field, code cells or a view of its own. The machine
// (internal/noticeslot) checks the description and the answer; the page draws only these ready elements.
//
// Never a password, a key or a sign-in: no kind has a password field, a view of its own is plain layout and buttons (no
// field, no script, no link), and a typed text that looks like a key stays here with a pointer to the safe.
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import CodeField, { CODE_FIELD, type TCodeFieldState } from '@/next/ui/code-field';
import { looksSecret } from './looks-secret';

export const SLOT_KIND = {
  status: 'status',
  progress: 'progress',
  choice: 'choice',
  confirm: 'confirm',
  field: 'field',
  code: 'code',
  custom: 'custom',
} as const;
export type TSlotKind = (typeof SLOT_KIND)[keyof typeof SLOT_KIND];

export const SLOT_SOURCE = { app: 'app', agent: 'agent' } as const;
export type TSlotSource = (typeof SLOT_SOURCE)[keyof typeof SLOT_SOURCE];

/** Who asks: the label over the plate, which the caller cannot change. */
export interface ISlotSource {
  kind: TSlotSource;
  id: string;
  name: string;
}

/** An open notice of the slot as the machine lists it (GET /api/platform/notices). */
export interface ISlotNotice {
  id: string;
  kind: TSlotKind;
  title: string;
  text?: string;
  icon?: string;
  options?: readonly string[];
  own?: boolean;
  yes?: string;
  no?: string;
  danger?: boolean;
  placeholder?: string;
  digits?: number;
  progress?: number;
  view?: string;
  source: ISlotSource;
  conversation?: string;
  createdAt: number;
  expiresAt: number;
  queued?: boolean;
}

/** What became of an answer: taken, refused as looking like a key, or not sent. */
export const SLOT_ANSWER = { ok: 'ok', secret: 'secret', failed: 'failed' } as const;
export type TSlotAnswer = (typeof SLOT_ANSWER)[keyof typeof SLOT_ANSWER];

const ICON_OF: Record<TSlotKind, string> = {
  status: 'info',
  progress: 'loader-circle',
  choice: 'list-checks',
  confirm: 'circle-question-mark',
  field: 'square-pen',
  code: 'key-round',
  custom: 'layout-grid',
};

export const slotIcon = (n: ISlotNotice): string => n.icon ?? ICON_OF[n.kind];

/** The kinds that wait for the person (the others only tell). */
export const slotWaits = (kind: TSlotKind): boolean =>
  kind !== SLOT_KIND.status && kind !== SLOT_KIND.progress;

/** «Приложение» or «Агент», over the source's name. */
export const sourceLabel = (s: ISlotSource): string =>
  t(s.kind === SLOT_SOURCE.app ? 'next.smart.slot.fromApp' : 'next.smart.slot.fromAgent');

/** What a view of its own may hold: plain text, layout and buttons. No script, no style, no link, no frame and no
 *  field of any kind — a view cannot ask for a password, go to another page or run code; a field is the field kind. */
const VIEW_TAGS: ReadonlySet<string> = new Set([
  'div',
  'span',
  'p',
  'b',
  'strong',
  'i',
  'em',
  'small',
  'br',
  'hr',
  'ul',
  'ol',
  'li',
  'h3',
  'h4',
  'button',
  'img',
]);
const VIEW_ATTRS: ReadonlySet<string> = new Set([
  'class',
  'data-answer',
  'data-on',
  'data-tone',
  'alt',
  'aria-label',
  'title',
]);
const VIEW_IMAGE = /^data:image\/(png|jpeg|webp|gif);base64,[A-Za-z0-9+/=]+$/;

/** Copies the allowed part of a view into this page's document: other tags leave only their text (a script or a
 *  style leaves nothing), other attributes and classes outside the kit (ink-*) are dropped. */
const viewNodes = (view: string): Node[] => {
  const parsed = new DOMParser().parseFromString(`<body>${view}</body>`, 'text/html').body;
  const copy = (node: Node): Node[] => {
    if (node.nodeType === Node.TEXT_NODE) return [document.createTextNode(node.textContent ?? '')];
    if (!(node instanceof Element)) return [];
    const tag = node.tagName.toLowerCase();
    if (tag === 'script' || tag === 'style' || tag === 'template') return [];
    const kids = [...node.childNodes].flatMap(copy);
    if (!VIEW_TAGS.has(tag)) return kids;
    const el = document.createElement(tag);
    for (const { name, value } of [...node.attributes]) {
      if (!VIEW_ATTRS.has(name)) continue;
      if (name === 'class') {
        const kit = value.split(/\s+/).filter((c) => c.startsWith('ink-'));
        if (kit.length > 0) el.className = kit.join(' ');
        continue;
      }
      el.setAttribute(name, value.slice(0, 2000));
    }
    if (tag === 'img') {
      const src = node.getAttribute('src') ?? '';
      if (!VIEW_IMAGE.test(src)) return [];
      el.setAttribute('src', src);
    }
    if (tag === 'button') el.setAttribute('type', 'button');
    el.append(...kids);
    return [el];
  };
  return [...parsed.childNodes].flatMap(copy);
};

/** What a pressed button answers: its data-answer as JSON, or as the text it is. */
const answerOf = (raw: string): unknown => {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return raw;
  }
};

/** A view of its own, drawn in the plate with its tokens and kit (styles/smart-notice.css .hx-sn__view); a button
 *  with data-answer answers. */
const SlotView = ({ view, onAnswer }: { view: string; onAnswer: (value: unknown) => void }) => {
  const box = useRef<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    box.current?.replaceChildren(...viewNodes(view));
  }, [view]);
  return (
    <div
      ref={box}
      className="hx-sn__view"
      role="group"
      aria-label={t('next.smart.slot.view')}
      onClick={(event) => {
        const pressed = (event.target as Element).closest('[data-answer]');
        if (pressed && box.current?.contains(pressed))
          onAnswer(answerOf(pressed.getAttribute('data-answer') ?? ''));
      }}
    />
  );
};

/** The open card's body for a notice of the slot. `field` is the card's own field (choice's own words, a field). */
const SlotBody = ({
  notice,
  busy,
  run,
  field,
  onSlot,
  onDismiss,
}: {
  notice: ISlotNotice;
  busy: boolean;
  run: (work: () => Promise<void>) => void;
  /** The card's field, drawn by the card (it carries the voice); `check` stops a text before it is sent. */
  field: (placeholder: string, onSend: (text: string) => void, check: (text: string) => boolean) => ReactNode;
  onSlot: (notice: ISlotNotice, value: unknown) => Promise<TSlotAnswer>;
  onDismiss: (notice: ISlotNotice) => Promise<void>;
}) => {
  const [secret, setSecret] = useState(false);
  const [code, setCode] = useState('');
  const [codeState, setCodeState] = useState<TCodeFieldState>(CODE_FIELD.idle);
  const send = (value: unknown) =>
    run(async () => {
      const took = await onSlot(notice, value);
      setSecret(took === SLOT_ANSWER.secret);
    });
  const check = (text: string) => {
    const bad = looksSecret(text);
    setSecret(bad);
    return !bad;
  };
  const warn = secret && (
    <p className="hx-sn__warn" role="alert">
      <Icon name="shield-x" size="sm" />
      {t('next.smart.slot.secret')}
    </p>
  );
  const head = (
    <>
      <p className="hx-sn__q">{notice.title}</p>
      {notice.text && <p className="hx-sn__why">{notice.text}</p>}
    </>
  );

  switch (notice.kind) {
    case SLOT_KIND.status:
      return head;
    case SLOT_KIND.progress:
      return (
        <>
          {head}
          <progress className="hx-sn__tone" value={notice.progress} max={1} aria-label={notice.title} />
        </>
      );
    case SLOT_KIND.choice:
      return (
        <>
          {head}
          <div className="hx-sn__opts">
            {(notice.options ?? []).map((label, i) => (
              <button
                key={label}
                type="button"
                className="hx-sn__opt"
                data-sn-action
                disabled={busy}
                onClick={() => send({ choice: i })}
              >
                {i < 9 && <kbd className="hx-sn__kbd">{i + 1}</kbd>}
                {label}
              </button>
            ))}
          </div>
          {notice.own && field(t('next.smart.ownAnswer'), (text) => send({ text }), check)}
          {warn}
        </>
      );
    case SLOT_KIND.confirm:
      // What cannot be undone is red and never the first action: Enter and the keyboard land on «no».
      return (
        <>
          {head}
          <div className="hx-sn__row">
            <button
              type="button"
              className="hx-sn__btn"
              data-tone="soft"
              data-sn-action={notice.danger ? '' : undefined}
              disabled={busy}
              onClick={() => send(false)}
            >
              {notice.no || t('next.smart.slot.no')}
            </button>
            <button
              type="button"
              className="hx-sn__btn"
              data-tone={notice.danger ? 'danger' : undefined}
              data-sn-action={notice.danger ? undefined : ''}
              disabled={busy}
              onClick={() => send(true)}
            >
              {notice.yes || t('next.smart.slot.yes')}
            </button>
          </div>
        </>
      );
    case SLOT_KIND.field:
      return (
        <>
          {head}
          {field(notice.placeholder || t('next.smart.slot.field'), (text) => send(text), check)}
          {warn}
        </>
      );
    case SLOT_KIND.code:
      return (
        <>
          {head}
          <div className="hx-sn__code">
            <CodeField
              fromMessages={false}
              value={code}
              onChange={(value) => {
                setCode(value);
                if (codeState === CODE_FIELD.invalid) setCodeState(CODE_FIELD.idle);
              }}
              onComplete={(value) => {
                setCodeState(CODE_FIELD.busy);
                void onSlot(notice, value).then((took) => {
                  if (took === SLOT_ANSWER.ok) {
                    setCodeState(CODE_FIELD.done);
                    return;
                  }
                  setCode('');
                  setCodeState(CODE_FIELD.invalid);
                });
              }}
              length={notice.digits ?? 6}
              label={notice.title}
              state={codeState}
            />
          </div>
          <div className="hx-sn__row">
            <button
              type="button"
              className="hx-sn__btn"
              data-tone="soft"
              disabled={busy}
              onClick={() => run(() => onDismiss(notice))}
            >
              {t('next.smart.cancel')}
            </button>
          </div>
        </>
      );
    case SLOT_KIND.custom:
      return (
        <>
          {head}
          {notice.view && <SlotView view={notice.view} onAnswer={send} />}
          {warn}
        </>
      );
  }
};

export default SlotBody;
