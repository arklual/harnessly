import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from 'react';
import { ChevronDown, ChevronUp, MessageSquarePlus } from 'lucide-react';
import { SplitSide, type DiffViewProps } from '@git-diff-view/react';
import {
  diffCommentLabel,
  lineKey,
  parseHunkLines,
  rangeBetween,
  type IDiffLine,
} from '@/lib/hl-diff-comment';
import type { TNewDiffComment } from '@/lib/hl-diff-comments-store';
import { t, plural } from '@/lib/i18n';

export interface IDiffCommenting {
  repo: string | null;
  /** Short sha when the diff is a commit's. */
  commit: string | null;
  onSubmit: (comment: TNewDiffComment) => void;
}

/** What the diff view needs to put the popover under the last selected line. */
export type TCommentViewProps = Pick<DiffViewProps<unknown>, 'renderWidgetLine' | 'onCreateUseWidgetHook'>;

type TWidgetHook = Parameters<NonNullable<DiffViewProps<unknown>['onCreateUseWidgetHook']>>[0];

interface ISelection {
  anchor: string;
  focus: string;
}

/** The comment typed so far; it outlives the popover, which is rebuilt under the new last line whenever the range changes. */
const createDraft = () => {
  let text = '';
  return {
    get: () => text,
    set: (value: string) => {
      text = value;
    },
  };
};

const NUMBER_CELL = 'td.diff-line-num';

const printedNumber = (row: Element, attribute: string): number | null => {
  const value = row.querySelector(`[${attribute}]`)?.getAttribute(attribute);
  return value ? Number(value) : null;
};

/** The row's identity is the pair of numbers the diff view prints in it; a side without a number is empty. */
const rowKey = (row: Element): string =>
  lineKey({ oldNo: printedNumber(row, 'data-line-old-num'), newNo: printedNumber(row, 'data-line-new-num') });

const numberCells = (root: Element): HTMLElement[] => [
  ...root.querySelectorAll<HTMLElement>(`tr.diff-line ${NUMBER_CELL}[role="button"]`),
];

interface IPopoverProps {
  file: string;
  lines: readonly IDiffLine[];
  /** The text typed so far, carried to the popover under the new last line when the range changes. */
  initial: string;
  onDraft: (text: string) => void;
  onStep: (delta: 1 | -1) => void;
  onSubmit: (comment: string) => void;
  onCancel: () => void;
}

const DiffCommentPopover = ({ file, lines, initial, onDraft, onStep, onSubmit, onCancel }: IPopoverProps) => {
  const [text, setText] = useState(initial);
  const formRef = useRef<HTMLFormElement>(null);
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  const ready = text.trim().length > 0;
  const label = diffCommentLabel({ file, lines });

  useEffect(() => {
    fieldRef.current?.focus();
  }, []);

  useEffect(() => {
    // Capture on window: a dialog around the changes closes on Escape in its own capture listener.
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape' || !formRef.current?.contains(document.activeElement)) return;
      event.preventDefault();
      event.stopPropagation();
      onCancel();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [onCancel]);

  const submit = () => {
    if (ready) onSubmit(text.trim());
  };
  const onFieldKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      submit();
    }
  };

  return (
    <form
      ref={formRef}
      className="tw-dc-pop"
      role="dialog"
      aria-label={t('agents.diffCommentLayer.commentOn', { label })}
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div className="tw-dc-pop-head">
        <span className="tw-dc-pop-title" title={file}>
          {label}
        </span>
        <small>{plural(lines.length, 'agents.diffCommentLayer.lines')}</small>
        <button
          type="button"
          className="tw-dc-pop-step"
          onClick={() => onStep(-1)}
          aria-label={t('agents.diffCommentLayer.edgeUpLabel')}
          title={t('agents.diffCommentLayer.edgeUp')}
        >
          <ChevronUp aria-hidden="true" />
        </button>
        <button
          type="button"
          className="tw-dc-pop-step"
          onClick={() => onStep(1)}
          aria-label={t('agents.diffCommentLayer.edgeDownLabel')}
          title={t('agents.diffCommentLayer.edgeDown')}
        >
          <ChevronDown aria-hidden="true" />
        </button>
      </div>
      <textarea
        ref={fieldRef}
        className="tw-dc-pop-field"
        rows={3}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          onDraft(event.target.value);
        }}
        onKeyDown={onFieldKey}
        aria-label={t('agents.diffCommentLayer.fieldLabel')}
        placeholder={t('agents.diffCommentLayer.placeholder')}
      />
      <div className="tw-dc-pop-actions">
        <button type="button" className="tw-dc-pop-cancel" onClick={onCancel}>
          {t('agents.diffCommentLayer.cancel')}
        </button>
        <button type="submit" className="tw-dc-pop-submit" disabled={!ready} title="Ctrl+Enter">
          <MessageSquarePlus aria-hidden="true" />
          {t('agents.diffCommentLayer.add')}
        </button>
      </div>
    </form>
  );
};

interface ILayerProps {
  file: string;
  hunks: readonly string[];
  commenting: IDiffCommenting;
  children: (view: TCommentViewProps) => ReactNode;
}

/**
 * Line selection over a unified diff of one file: a click on a line number selects it, Shift+click extends
 * the range, the popover under the last selected line takes the comment. The diff view draws everything; the rows are
 * marked in place and the popover is the view's own widget row.
 */
const DiffCommentLayer = ({ file, hunks, commenting, children }: ILayerProps) => {
  const [root, setRoot] = useState<HTMLDivElement | null>(null);
  const [selection, setSelection] = useState<ISelection | null>(null);
  const [widget, setWidget] = useState<{ hook: TWidgetHook } | null>(null);
  const onCreateWidgetHook = useCallback((hook: TWidgetHook) => setWidget({ hook }), []);
  const [draft] = useState(createDraft);
  const lines = useMemo(() => parseHunkLines(hunks), [hunks]);
  const known = useMemo(() => new Map(lines.map((line) => [lineKey(line), line])), [lines]);
  const selected = useMemo(
    () => (selection ? rangeBetween(lines, selection.anchor, selection.focus) : []),
    [lines, selection],
  );
  const selectedKeys = useMemo(() => new Set(selected.map(lineKey)), [selected]);

  useLayoutEffect(() => {
    if (!root) return;
    const mark = () => {
      let first = true;
      for (const row of root.querySelectorAll('tr.diff-line')) {
        const key = rowKey(row);
        row.classList.toggle('tw-dc-selected', selectedKeys.has(key));
        const cell = row.querySelector<HTMLElement>(NUMBER_CELL);
        const line = known.get(key);
        if (!cell || !line) continue;
        cell.setAttribute('role', 'button');
        cell.setAttribute(
          'aria-label',
          t(
            line.kind === 'del'
              ? 'agents.diffCommentLayer.commentDeletedLine'
              : 'agents.diffCommentLayer.commentLine',
            { line: String(line.newNo ?? line.oldNo) },
          ),
        );
        cell.setAttribute('aria-pressed', String(selectedKeys.has(key)));
        cell.tabIndex = first ? 0 : -1;
        first = false;
      }
    };
    mark();
    const observer = new MutationObserver(mark);
    observer.observe(root, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [root, known, selectedKeys]);

  const pick = (key: string, extend: boolean) => {
    setSelection((current) => {
      if (extend && current) return { anchor: current.anchor, focus: key };
      return current && current.anchor === key && current.focus === key ? null : { anchor: key, focus: key };
    });
  };

  const cellOf = (event: { target: EventTarget }) => {
    const cell = (event.target as Element).closest<HTMLElement>(NUMBER_CELL);
    const row = cell?.closest('tr.diff-line');
    const key = row ? rowKey(row) : null;
    return cell && key !== null && known.has(key) ? { cell, key } : null;
  };

  const onClick = (event: MouseEvent<HTMLDivElement>) => {
    const hit = cellOf(event);
    if (hit) pick(hit.key, event.shiftKey);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const hit = cellOf(event);
    if (!hit || event.target !== hit.cell) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      pick(hit.key, event.shiftKey);
    } else if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && root) {
      event.preventDefault();
      const cells = numberCells(root);
      const next = cells[cells.indexOf(hit.cell) + (event.key === 'ArrowDown' ? 1 : -1)];
      if (!next) return;
      hit.cell.tabIndex = -1;
      next.tabIndex = 0;
      next.focus();
    }
  };

  const step = useCallback(
    (delta: 1 | -1) => {
      setSelection((current) => {
        if (!current) return current;
        const at = lines.findIndex((line) => lineKey(line) === current.focus);
        const next = lines[at + delta];
        return next ? { anchor: current.anchor, focus: lineKey(next) } : current;
      });
    },
    [lines],
  );

  const dropSelection = useCallback(() => {
    draft.set('');
    setSelection(null);
  }, [draft]);

  const last = selected.at(-1);
  useEffect(() => {
    if (!widget) return;
    const { setWidget: place } = widget.hook.getReadonlyState();
    if (!last) place({});
    else if (last.newNo !== null) place({ side: SplitSide.new, lineNumber: last.newNo });
    else place({ side: SplitSide.old, lineNumber: last.oldNo ?? undefined });
  }, [widget, last]);

  const close = useCallback(() => {
    const back =
      selection && root
        ? numberCells(root).find((cell) => {
            const row = cell.closest('tr.diff-line');
            return row ? rowKey(row) === selection.focus : false;
          })
        : undefined;
    dropSelection();
    back?.focus();
  }, [selection, root, dropSelection]);

  const renderWidgetLine = useCallback<NonNullable<TCommentViewProps['renderWidgetLine']>>(
    () => (
      <DiffCommentPopover
        file={file}
        lines={selected}
        initial={draft.get()}
        onDraft={draft.set}
        onStep={step}
        onCancel={close}
        onSubmit={(comment) => {
          commenting.onSubmit({
            file,
            repo: commenting.repo,
            commit: commenting.commit,
            lines: selected,
            comment,
          });
          dropSelection();
        }}
      />
    ),
    [file, selected, commenting, draft, step, close, dropSelection],
  );

  return (
    <div ref={setRoot} className="tw-dc" onClick={onClick} onKeyDown={onKeyDown}>
      {children({ renderWidgetLine, onCreateUseWidgetHook: onCreateWidgetHook })}
    </div>
  );
};

export default DiffCommentLayer;
