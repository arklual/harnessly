// ⌘/Ctrl + click on a name in the diff: where it is declared among the files of the change, as a card over the diff
// with the declaration's line; a click on a place opens its file there. The files' texts are read as the review reads
// them (both whole versions when the source can give them, the patch otherwise), once per version.
import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { useCallback, useEffect, useRef, useState } from 'react';
import { findDefinitions, wordAt, type IDefinition } from '@/next/features/review/app/definitions';
import { FILE_STATUS, type IReviewFile, type IReviewSource } from '@/next/features/review/model';
import { sidesFromPatch } from '@/next/features/review/patch';
import { RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const EXIT_MS = 160;
/** Files read whole for a lookup; past this the rest are searched in their patches. */
const READ_WHOLE = 40;

type TPeek = { word: string; found: IDefinition[] | null };

export const useDefinitions = (source: IReviewSource, file: IReviewFile | null) => {
  const [peek, setPeek] = useState<TPeek | null>(null);
  const texts = useRef(new Map<string, string>());
  const fileRef = useRef(file);
  const sourceRef = useRef(source);
  useEffect(() => {
    fileRef.current = file;
    sourceRef.current = source;
  }, [file, source]);

  const lookup = useCallback(async (word: string) => {
    setPeek({ word, found: null });
    const src = sourceRef.current;
    const files = src.files.filter((f) => !f.binary && f.status !== FILE_STATUS.deleted);
    const read = await Promise.all(
      files.map(async (f, i) => {
        const key = `${f.path}\0${f.version}`;
        const kept = texts.current.get(key);
        if (kept !== undefined) return { path: f.path, text: kept };
        const whole = i < READ_WHOLE && src.loadContent ? await src.loadContent(f).catch(() => null) : null;
        const text = whole?.after ?? sidesFromPatch(f.patch).after;
        texts.current.set(key, text);
        return { path: f.path, text };
      }),
    );
    setPeek((p) =>
      p?.word === word ? { word, found: findDefinitions(word, read, fileRef.current?.path) } : p,
    );
  }, []);

  /** The editors' part: a click with ⌘/Ctrl on a name looks it up. */
  const extension = useCallback(
    (): Extension =>
      EditorView.domEventHandlers({
        mousedown: (event, view) => {
          if (!(event.metaKey || event.ctrlKey) || event.button !== 0) return false;
          const pos = view.posAtCoords({ x: event.clientX, y: event.clientY });
          if (pos === null) return false;
          const line = view.state.doc.lineAt(pos);
          const word = wordAt(line.text, pos - line.from);
          if (!word) return false;
          event.preventDefault();
          void lookup(word);
          return true;
        },
      }),
    [lookup],
  );

  const close = useCallback(() => setPeek(null), []);
  return { peek, extension, close };
};

interface IDefinitionPeekProps {
  peek: TPeek | null;
  files: readonly IReviewFile[];
  onOpen: (file: IReviewFile, line: number) => void;
  onClose: () => void;
}

export const DefinitionPeek = ({ peek, files, onOpen, onClose }: IDefinitionPeekProps) => {
  const shown = usePresence(peek, EXIT_MS);
  useEffect(() => {
    if (!peek) return undefined;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [peek, onClose]);
  if (!shown.value) return null;
  const { word, found } = shown.value;
  return (
    <div
      className="hx-rvdef"
      role="dialog"
      aria-label={t('next.reviewApp.def.title', { name: word })}
      data-state={shown.state}
    >
      <div className="hx-rvdef__head">
        <Icon name="locate" size="sm" />
        <span className="hx-rvdef__title">
          {t('next.reviewApp.def.title', { name: '' })}
          <code>{word}</code>
        </span>
        <RoundButton
          icon="x"
          label={t('next.reviewApp.def.close')}
          size="sm"
          variant="ghost"
          onClick={onClose}
        />
      </div>
      {found === null && <p className="hx-rvdef__note">{t('next.review.loading')}</p>}
      {found?.length === 0 && <p className="hx-rvdef__note">{t('next.reviewApp.def.none')}</p>}
      {found && found.length > 0 && (
        <ol className="hx-rvdef__list">
          {found.map((d) => {
            const target = files.find((f) => f.path === d.path);
            return (
              <li key={`${d.path}:${d.line}`}>
                <button
                  type="button"
                  className="hx-rvdef__row"
                  disabled={!target}
                  onClick={() => {
                    if (!target) return;
                    onOpen(target, d.line);
                    onClose();
                  }}
                >
                  <span className="hx-rvdef__where">
                    {d.path}:{d.line}
                  </span>
                  <code className="hx-rvdef__code">{d.text}</code>
                </button>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
};
