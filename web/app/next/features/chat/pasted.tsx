// A pasted block inside a message, as Claude Code records it: <pasted_content id="…">text</pasted_content id="…">.
// The person's bubble and another agent's card fold it the same way.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

/** A pasted block as Claude Code writes it: <pasted_content id="…">text</pasted_content id="…">. */
const PASTED = /<pasted_content id="([^"]+)">\n?([\s\S]*?)\n?<\/pasted_content id="\1">/g;

interface ITextPart {
  key: string;
  text: string;
  pasted: boolean;
}

/** The message split into what the person typed and what they pasted. */
export const splitPasted = (text: string): ITextPart[] => {
  const parts: ITextPart[] = [];
  let at = 0;
  for (const m of text.matchAll(PASTED)) {
    const before = text.slice(at, m.index).trim();
    if (before) parts.push({ key: `t${at}`, text: before, pasted: false });
    parts.push({ key: `p${m.index}`, text: m[2].trim(), pasted: true });
    at = (m.index ?? 0) + m[0].length;
  }
  const rest = text.slice(at).trim();
  if (rest) parts.push({ key: `t${at}`, text: rest, pasted: false });
  return parts;
};

/**
 * What the bubble shows of a message. Claude Code wraps a long or fast burst in pasted_content whoever made it (a
 * dictated text the typist types in one go looks like a paste to it), so the wrapper alone proves nothing: a message
 * the person sent from this app (own: the outbox's clientId came back on its entry) is their own words, shown as the
 * bubble's plain text; only one typed straight into the agent's terminal can still be a real paste.
 */
export const bubbleParts = (text: string, own: boolean): ITextPart[] => {
  const parts = splitPasted(text);
  return own ? parts.map((part) => ({ ...part, pasted: false })) : parts;
};

/** Pasted text: a quiet block, folded to a few lines, «Показать целиком» unfolds it. */
export const Pasted = ({ text }: { text: string }) => {
  const [open, setOpen] = useState(false);
  const long = text.split('\n').length > 6 || text.length > 480;
  return (
    <div className="hx-paste" data-open={open || !long || undefined}>
      <div className="hx-paste__label">
        <Icon name="text-quote" size="sm" />
        {t('next.message.pasted')}
      </div>
      <div className="hx-paste__text">{text}</div>
      {long && (
        <button type="button" className="hx-paste__more" onClick={() => setOpen((v) => !v)}>
          {open ? t('next.message.pastedLess') : t('next.message.pastedMore')}
        </button>
      )}
    </div>
  );
};
