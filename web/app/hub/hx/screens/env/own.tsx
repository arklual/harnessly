// «Добавить своё» of a member: their own skill (the text of SKILL.md) or MCP server (an http address). The hub puts it in
// the person's own layer and the machines take it with the rest of their environment.
import { useState } from 'react';

import type { MeEnvOwnRequest } from '../../../../hub-api/env';
import { Btn, Icon } from '../../ui';
import { Seg } from './bits';
import { Field } from './form';

type Kind = MeEnvOwnRequest['kind'];

export const OwnAdd = ({ onAdd }: { onAdd: (req: MeEnvOwnRequest) => Promise<boolean> }) => {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<Kind>('skill');
  const [name, setName] = useState('');
  const [hint, setHint] = useState('');
  const [text, setText] = useState('');
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);

  if (!open)
    return (
      <button type="button" className="ev-addrow" onClick={() => setOpen(true)}>
        <Icon i="plus" size="sm" />
        Добавить своё
      </button>
    );

  const value = kind === 'skill' ? text : url;
  const ready = name.trim() !== '' && value.trim() !== '';
  const save = async () => {
    setBusy(true);
    const ok = await onAdd({
      kind,
      name: name.trim(),
      ...(hint.trim() ? { hint: hint.trim() } : {}),
      source: kind === 'skill' ? { body: text } : { transport: 'http', url: url.trim() },
    });
    setBusy(false);
    if (!ok) return;
    setName('');
    setHint('');
    setText('');
    setUrl('');
    setOpen(false);
  };

  return (
    <div className="ev-layer" data-block="Добавить своё">
      <Field label="Что добавить">
        <Seg
          items={[
            ['skill', 'Скилл'],
            ['mcp', 'MCP-сервер'],
          ]}
          value={kind}
          onChange={setKind}
          label="Что добавить"
        />
      </Field>
      <Field label="Название">
        <input
          className="hx-set__input"
          value={name}
          aria-label="Название"
          onChange={(e) => setName(e.target.value)}
        />
      </Field>
      <Field label="Коротко: для чего">
        <input
          className="hx-set__input"
          value={hint}
          aria-label="Коротко: для чего"
          onChange={(e) => setHint(e.target.value)}
        />
      </Field>
      {kind === 'skill' ? (
        <Field label="Текст скилла (SKILL.md)">
          <textarea
            className="hx-set__input hx-set__input--mono ev-ta"
            rows={8}
            value={text}
            aria-label="Текст скилла"
            onChange={(e) => setText(e.target.value)}
          />
        </Field>
      ) : (
        <Field label="Адрес (http)">
          <input
            className="hx-set__input hx-set__input--mono"
            value={url}
            placeholder="https://mcp.example.ru/mcp"
            aria-label="Адрес MCP-сервера"
            onChange={(e) => setUrl(e.target.value)}
          />
        </Field>
      )}
      <Btn variant="primary" size="sm" disabled={!ready || busy} onClick={() => void save()}>
        Добавить
      </Btn>
      <Btn variant="ghost" size="sm" disabled={busy} onClick={() => setOpen(false)}>
        Отмена
      </Btn>
    </div>
  );
};
