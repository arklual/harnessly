// A spreadsheet in the pane: the file's sheets as a grid that draws only what is in view, with its frozen rows and columns,
// its styles and the sum of a selection. A cell is edited in place or in the bar above; the file is saved by itself a moment
// after the last edit. Its own chunk: the engine comes only when a spreadsheet is opened.
import {
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { cellsSelection, MAX_ROWS, type IFileSelection } from '@/next/features/pane/file-selection';
import FileHistory from '@/next/features/pane/file-history';
import { addressOf, columnName } from '@/next/features/pane/sheet/sheet-names';
import type { TSheetFile } from '@/next/features/pane/sheet/sheet-api';
import { plainNumber } from '@/next/features/pane/sheet/sheet-format';
import { useSheetFile } from '@/next/features/pane/sheet/use-sheet-file';
import { SAVE, SaveConflict, SaveWord } from '@/next/features/pane/file-save-state';
import {
  clampPlace,
  columnAt,
  columnOffsets,
  columnPixels,
  fromClipboard,
  HEAD_WIDTH,
  inRange,
  MIN_COLUMNS,
  MIN_ROWS,
  OVERSCAN,
  parseCellPlace,
  rangeOf,
  ROW_HEIGHT,
  stylesToCss,
  toClipboard,
  type ICellPlace,
  type IRangeStats,
  type ISelection,
} from '@/next/features/pane/sheet/sheet-model';

export interface ISheetViewProps {
  machine: MachineId;
  path: string;
  src: string;
  name: string;
  kind: TSheetFile;
  saveHref: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** A cell or range the agent asked to show; onPlaced when it is there. */
  place?: string | null;
  onPlaced?: () => void;
}

interface IEditing {
  place: ICellPlace;
  value: string;
}

interface IUndo {
  row: number;
  column: number;
  input: string;
}

const REPORT_PAUSE_MS = 200;
const START: ISelection = { anchor: { row: 0, column: 0 }, focus: { row: 0, column: 0 } };

const SheetView = ({
  machine,
  path,
  src,
  name,
  kind,
  saveHref,
  onSelection,
  place,
  onPlaced,
}: ISheetViewProps) => {
  const file = useSheetFile({ machine, path, src, name, kind });
  const { shape, rowOf, rowsOf, edit, version } = file;
  const scroller = useRef<HTMLDivElement | null>(null);
  const grid = useRef<HTMLDivElement | null>(null);
  const dragging = useRef(false);
  const undo = useRef<IUndo[][]>([]);
  const liveEdit = useRef<IEditing | null>(null);
  const fromAgent = useRef<ISelection | null>(null);
  const [view, setView] = useState({ top: 0, left: 0, width: 800, height: 500 });
  const [selection, setSelection] = useState<ISelection>(START);
  const [editing, setEditing] = useState<IEditing | null>(null);
  const [stats, setStats] = useState<IRangeStats | null>(null);

  const rowCount = Math.max(MIN_ROWS, (shape?.rows ?? 0) + 20);
  const columnCount = Math.max(MIN_COLUMNS, (shape?.columns ?? 0) + 4);

  const widths = useMemo(
    () => Array.from({ length: columnCount }, (_, at) => columnPixels(shape?.widths.get(at))),
    [columnCount, shape],
  );
  const offsets = useMemo(() => columnOffsets(widths), [widths]);
  const frozenRows = shape?.frozenRows ?? 0;
  const frozenColumns = shape?.frozenColumns ?? 0;
  const frozenWidth = offsets[frozenColumns] ?? 0;
  const frozenHeight = frozenRows * ROW_HEIGHT;

  useEffect(() => {
    const box = scroller.current;
    if (!box) return;
    const measure = () =>
      setView((before) => ({ ...before, width: box.clientWidth, height: box.clientHeight }));
    const observer = new ResizeObserver(measure);
    observer.observe(box);
    return () => observer.disconnect();
  }, []);

  // The file's own styles, as a sheet of their own that leaves with the view.
  useEffect(() => {
    const element = document.createElement('style');
    element.textContent = stylesToCss(file.styles);
    document.head.append(element);
    return () => element.remove();
  }, [file.styles]);

  const scrollFrame = useRef<number | null>(null);
  const onScroll = () => {
    if (scrollFrame.current !== null) return;
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = null;
      const box = scroller.current;
      if (box) setView((before) => ({ ...before, top: box.scrollTop, left: box.scrollLeft }));
    });
  };

  const firstRow = Math.max(frozenRows, Math.floor(view.top / ROW_HEIGHT) - OVERSCAN);
  const lastRow = Math.min(rowCount - 1, Math.ceil((view.top + view.height) / ROW_HEIGHT) + OVERSCAN);
  const firstColumn = Math.max(frozenColumns, columnAt(offsets, view.left + frozenWidth) - 1);
  const lastColumn = Math.min(columnCount - 1, columnAt(offsets, view.left + view.width - HEAD_WIDTH) + 1);

  const shownColumns = useMemo(() => {
    const list: number[] = [];
    for (let at = 0; at < frozenColumns; at += 1) list.push(at);
    for (let at = firstColumn; at <= lastColumn; at += 1) list.push(at);
    return list;
  }, [frozenColumns, firstColumn, lastColumn]);

  // The columns of every row drawn: the row's number, the frozen, a gap for those scrolled past, the visible, the rest.
  const template = useMemo(() => {
    const frozen = widths.slice(0, frozenColumns).map((w) => `${w}px`);
    const visible = widths.slice(firstColumn, lastColumn + 1).map((w) => `${w}px`);
    const before = (offsets[firstColumn] ?? 0) - frozenWidth;
    const after = (offsets[columnCount] ?? 0) - (offsets[lastColumn + 1] ?? 0);
    return [`${HEAD_WIDTH}px`, ...frozen, `${before}px`, ...visible, `${after}px`].join(' ');
  }, [widths, offsets, frozenColumns, frozenWidth, firstColumn, lastColumn, columnCount]);

  useEffect(() => {
    grid.current?.style.setProperty('--cols', template);
    grid.current?.style.setProperty('--grid-w', `${HEAD_WIDTH + (offsets[columnCount] ?? 0)}px`);
    grid.current?.style.setProperty('--grid-h', `${ROW_HEIGHT * (rowCount + 1)}px`);
    grid.current?.style.setProperty('--frozen-h', `${frozenHeight + ROW_HEIGHT}px`);
  }, [template, offsets, columnCount, rowCount, frozenHeight]);

  const range = rangeOf(selection);
  const focus = selection.focus;
  const focusCell = rowOf(focus.row)?.[focus.column] ?? null;
  const barValue = editing ? editing.value : (focusCell?.input ?? '');
  const many = range.top !== range.bottom || range.left !== range.right;

  // What a selection adds up to. Long columns are read from the worker, not only what is on screen.
  useEffect(() => {
    if (!shape) return;
    const { top, bottom, left, right } = range;
    let stopped = false;
    if (top === bottom && left === right) {
      void Promise.resolve().then(() => {
        if (!stopped) setStats(null);
      });
      return () => {
        stopped = true;
      };
    }
    void file.statsOf(range).then((next) => {
      if (!stopped) setStats(next);
    });
    return () => {
      stopped = true;
    };
    // version: a counted cell changes the sum.
  }, [range.top, range.bottom, range.left, range.right, shape, file.statsOf, version]); // eslint-disable-line react-hooks/exhaustive-deps

  const scrollIntoView = (at: ICellPlace) => {
    const box = scroller.current;
    if (!box) return;
    const top = at.row * ROW_HEIGHT;
    const bottom = top + ROW_HEIGHT;
    if (at.row >= frozenRows) {
      if (top < box.scrollTop + frozenHeight) box.scrollTop = Math.max(0, top - frozenHeight);
      else if (bottom + ROW_HEIGHT > box.scrollTop + box.clientHeight)
        box.scrollTop = bottom + ROW_HEIGHT - box.clientHeight;
    }
    const left = offsets[at.column] ?? 0;
    const right = offsets[at.column + 1] ?? left;
    if (at.column >= frozenColumns) {
      if (left < box.scrollLeft + frozenWidth) box.scrollLeft = Math.max(0, left - frozenWidth);
      else if (right + HEAD_WIDTH > box.scrollLeft + box.clientWidth)
        box.scrollLeft = right + HEAD_WIDTH - box.clientWidth;
    }
  };

  // What the person selected goes to the chat a moment after it settles. The cell a sheet opens on is no selection of
  // theirs, and neither is a place the agent asked to show.
  const ready = shape !== null;
  const reportNow = useEffectEvent(async () => {
    if (selection === START) {
      onSelection?.(null);
      return;
    }
    if (fromAgent.current === selection) return;
    const [rows, sum] = await Promise.all([
      rowsOf(range.top, Math.min(range.bottom, range.top + MAX_ROWS - 1)),
      file.statsOf(range),
    ]);
    onSelection?.(
      cellsSelection({
        sheet: file.sheets.length > 1 ? (file.sheets[file.sheet] ?? null) : null,
        range,
        rows,
        stats: sum,
      }),
    );
  });
  useEffect(() => {
    if (!ready) return;
    const timer = setTimeout(() => void reportNow(), REPORT_PAUSE_MS);
    return () => clearTimeout(timer);
  }, [selection, ready]);

  const goToPlace = useEffectEvent((asked: string) => {
    const found = parseCellPlace(asked, file.sheets);
    if (found?.sheet !== null && found?.sheet !== undefined && found.sheet !== file.sheet) {
      void file.openSheet(found.sheet);
      return;
    }
    if (found) {
      const chosen = {
        anchor: { row: found.range.top, column: found.range.left },
        focus: { row: found.range.bottom, column: found.range.right },
      };
      fromAgent.current = chosen;
      queueMicrotask(() => setSelection(chosen));
      scrollIntoView(chosen.anchor);
    }
    onPlaced?.();
  });
  useEffect(() => {
    if (place && ready) goToPlace(place);
  }, [place, ready, file.sheet]);

  const select = (place: ICellPlace, extend: boolean) => {
    const next = clampPlace(place, rowCount, columnCount);
    setSelection((before) => ({ anchor: extend ? before.anchor : next, focus: next }));
    scrollIntoView(next);
  };

  /** Writes the cells, remembering what they held so an undo can put it back. */
  const write = async (edits: { row: number; column: number; input: string }[]) => {
    if (edits.length === 0) return;
    const first = Math.min(...edits.map(({ row }) => row));
    const last = Math.max(...edits.map(({ row }) => row));
    const held = await rowsOf(first, last);
    undo.current.push(
      edits.map(({ row, column }) => ({ row, column, input: held[row - first]?.[column]?.input ?? '' })),
    );
    await edit(edits);
  };

  // What is being edited, as the handlers must see it: leaving the input fires a blur before the next render, and an edit
  // already written or cancelled must not be written by it.
  const putEditing = (next: IEditing | null) => {
    liveEdit.current = next;
    setEditing(next);
  };

  const commit = async (next?: ICellPlace) => {
    const open = liveEdit.current;
    if (!open) return;
    putEditing(null);
    grid.current?.focus();
    const had = rowOf(open.place.row)?.[open.place.column]?.input ?? '';
    if (open.value !== had)
      await write([{ row: open.place.row, column: open.place.column, input: open.value }]);
    if (next) select(next, false);
  };

  const cancelEdit = () => {
    putEditing(null);
    grid.current?.focus();
  };

  const copy = async () => {
    const rows = await rowsOf(range.top, range.bottom);
    const out: string[][] = [];
    for (let row = range.top; row <= range.bottom; row += 1) {
      const line: string[] = [];
      for (let column = range.left; column <= range.right; column += 1)
        line.push(rows[row - range.top]?.[column]?.text ?? '');
      out.push(line);
    }
    await navigator.clipboard.writeText(toClipboard(out));
  };

  const clearRange = async () => {
    const rows = await rowsOf(range.top, range.bottom);
    const edits: { row: number; column: number; input: string }[] = [];
    rows.forEach((cells, down) => {
      for (let column = range.left; column <= range.right; column += 1)
        if (cells[column]) edits.push({ row: range.top + down, column, input: '' });
    });
    await write(edits);
  };

  const onGridKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (editing) return;
    const { row, column } = focus;
    const meta = event.ctrlKey || event.metaKey;
    if (meta && event.code === 'KeyC') {
      event.preventDefault();
      void copy();
      return;
    }
    if (meta && event.code === 'KeyV') {
      event.preventDefault();
      void navigator.clipboard.readText().then((text) => {
        const edits = fromClipboard(text).flatMap((line, down) =>
          line.map((input, across) => ({ row: range.top + down, column: range.left + across, input })),
        );
        void write(edits);
      });
      return;
    }
    if (meta && event.code === 'KeyZ') {
      event.preventDefault();
      const last = undo.current.pop();
      if (last) void edit(last);
      return;
    }
    if (meta && event.code === 'KeyA') {
      event.preventDefault();
      setSelection({
        anchor: { row: 0, column: 0 },
        focus: { row: Math.max(0, (shape?.rows ?? 1) - 1), column: Math.max(0, (shape?.columns ?? 1) - 1) },
      });
      return;
    }
    const moves: Record<string, ICellPlace> = {
      ArrowUp: { row: row - 1, column },
      ArrowDown: { row: row + 1, column },
      ArrowLeft: { row, column: column - 1 },
      ArrowRight: { row, column: column + 1 },
      PageUp: { row: row - 20, column },
      PageDown: { row: row + 20, column },
      Home: { row, column: 0 },
      End: { row, column: Math.max(0, (shape?.columns ?? 1) - 1) },
    };
    const move = moves[event.key];
    if (move) {
      event.preventDefault();
      select(move, event.shiftKey);
      return;
    }
    if (event.key === 'Tab') {
      event.preventDefault();
      select({ row, column: column + (event.shiftKey ? -1 : 1) }, false);
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      select({ row: row + (event.shiftKey ? -1 : 1), column }, false);
      return;
    }
    if (event.key === 'F2') {
      event.preventDefault();
      putEditing({ place: focus, value: focusCell?.input ?? '' });
      return;
    }
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      void clearRange();
      return;
    }
    if (event.key.length === 1 && !meta && !event.altKey) {
      event.preventDefault();
      putEditing({ place: focus, value: event.key });
    }
  };

  if (file.failed !== null)
    return (
      <div className="hx-media__failed" role="status">
        <Icon name="file-spreadsheet" />
        <span className="hx-media__failed-title">{t('next.sheet.failed')}</span>
        <a className="hx-media__failed-save" href={saveHref} download>
          {t('next.delivered.save')}
        </a>
      </div>
    );

  /** The cell an event in the grid happened on. One listener on the grid, not one on each cell drawn. */
  const placeOf = (target: EventTarget): ICellPlace | null => {
    const cell = target instanceof Element ? target.closest<HTMLElement>('[data-row]') : null;
    if (!cell) return null;
    return { row: Number(cell.dataset.row), column: Number(cell.dataset.column) };
  };

  const onGridPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    grid.current?.focus();
    const place = placeOf(event.target);
    if (event.button !== 0 || !place) return;
    if (editing?.place.row === place.row && editing.place.column === place.column) return;
    void commit();
    dragging.current = true;
    setSelection((before) => ({ anchor: event.shiftKey ? before.anchor : place, focus: place }));
  };

  const onGridPointerOver = (event: PointerEvent<HTMLDivElement>) => {
    const place = dragging.current ? placeOf(event.target) : null;
    if (place) setSelection((before) => ({ anchor: before.anchor, focus: place }));
  };

  const onGridDoubleClick = (event: MouseEvent<HTMLDivElement>) => {
    const place = placeOf(event.target);
    if (place) putEditing({ place, value: rowOf(place.row)?.[place.column]?.input ?? '' });
  };

  const onEditChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (liveEdit.current) putEditing({ ...liveEdit.current, value: event.target.value });
  };

  const onEditBlur = () => void commit();

  const onEditKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (!editing) return;
    const { row, column } = editing.place;
    if (event.key === 'Enter') {
      event.preventDefault();
      void commit({ row: row + (event.shiftKey ? -1 : 1), column });
    } else if (event.key === 'Tab') {
      event.preventDefault();
      void commit({ row, column: column + (event.shiftKey ? -1 : 1) });
    } else if (event.key === 'Escape') {
      event.preventDefault();
      cancelEdit();
    }
  };

  const drawRow = (row: number): ReactNode => {
    const cells = rowOf(row);
    return (
      <div
        key={row}
        className="hx-cells__row"
        role="row"
        aria-rowindex={row + 1}
        data-frozen={row < frozenRows ? 'true' : undefined}
        ref={(element) => element?.style.setProperty('--y', `${row * ROW_HEIGHT}px`)}
      >
        <div
          className="hx-cells__rownum"
          role="rowheader"
          data-on={row >= range.top && row <= range.bottom ? 'true' : undefined}
        >
          {row + 1}
        </div>
        {shownColumns.map((column, at) => {
          const cell = cells?.[column] ?? null;
          const isEditing = editing?.place.row === row && editing.place.column === column;
          const isFrozen = column < frozenColumns;
          // The gap for the columns scrolled past stands after the frozen ones.
          const gap = at === frozenColumns ? <div key="gap" aria-hidden="true" /> : null;
          return (
            <span key={column} className="hx-cells__slot">
              {gap}
              <div
                className="hx-cells__cell"
                role="gridcell"
                data-s={cell?.style || undefined}
                data-num={cell?.number ? 'true' : undefined}
                data-frozen={isFrozen ? 'true' : undefined}
                data-sel={inRange(range, row, column) ? 'true' : undefined}
                data-focus={focus.row === row && focus.column === column ? 'true' : undefined}
                ref={(element) => {
                  if (isFrozen) element?.style.setProperty('--l', `${HEAD_WIDTH + (offsets[column] ?? 0)}px`);
                }}
                data-row={row}
                data-column={column}
              >
                {isEditing ? (
                  <input
                    className="hx-cells__edit"
                    autoFocus
                    value={editing.value}
                    aria-label={addressOf(row, column)}
                    onChange={onEditChange}
                    onBlur={onEditBlur}
                    onKeyDown={onEditKey}
                  />
                ) : (
                  cell?.text
                )}
              </div>
            </span>
          );
        })}
      </div>
    );
  };

  const bodyRows: ReactNode[] = [];
  for (let row = firstRow; row <= lastRow; row += 1) bodyRows.push(drawRow(row));
  const frozenBlock: ReactNode[] = [];
  for (let row = 0; row < frozenRows; row += 1) frozenBlock.push(drawRow(row));

  return (
    <div
      className="hx-cells"
      onPointerUp={() => {
        dragging.current = false;
      }}
    >
      <div className="hx-cells__bar">
        <span className="hx-cells__address">
          {many
            ? `${addressOf(range.top, range.left)}:${addressOf(range.bottom, range.right)}`
            : addressOf(focus.row, focus.column)}
        </span>
        <input
          className="hx-cells__formula"
          aria-label={t('next.sheet.formula')}
          value={barValue}
          onFocus={() => {
            if (!liveEdit.current) putEditing({ place: focus, value: focusCell?.input ?? '' });
          }}
          onChange={(event) =>
            putEditing({ place: liveEdit.current?.place ?? focus, value: event.target.value })
          }
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              void commit({ row: focus.row + 1, column: focus.column });
            } else if (event.key === 'Escape') {
              event.preventDefault();
              cancelEdit();
            }
          }}
          onBlur={() => void commit()}
        />
        <FileHistory machine={machine} path={path} current={file.revision} onRestored={file.reload} />
        <SaveWord save={file.save} />
      </div>
      {file.save === SAVE.conflict && (
        <SaveConflict
          onReload={() => {
            undo.current = [];
            file.reload();
          }}
          onOverwrite={file.overwrite}
        />
      )}
      <div className="hx-cells__scroll" ref={scroller} onScroll={onScroll}>
        {shape === null && (
          <span className="hx-sk hx-cells__wait" role="status" aria-label={t('next.sheet.opening')} />
        )}
        <div
          ref={grid}
          className="hx-cells__grid"
          role="grid"
          aria-label={name}
          aria-rowcount={rowCount}
          aria-colcount={columnCount}
          aria-busy={shape === null}
          tabIndex={0}
          onKeyDown={onGridKey}
          onPointerDown={onGridPointerDown}
          onPointerOver={onGridPointerOver}
          onDoubleClick={onGridDoubleClick}
        >
          <div className="hx-cells__head" role="row">
            <div className="hx-cells__corner" aria-hidden="true" />
            {shownColumns.map((column, at) => (
              <span key={column} className="hx-cells__slot">
                {at === frozenColumns && <div aria-hidden="true" />}
                <div
                  className="hx-cells__colname"
                  role="columnheader"
                  data-frozen={column < frozenColumns ? 'true' : undefined}
                  data-on={column >= range.left && column <= range.right ? 'true' : undefined}
                  ref={(element) => {
                    if (column < frozenColumns)
                      element?.style.setProperty('--l', `${HEAD_WIDTH + (offsets[column] ?? 0)}px`);
                  }}
                >
                  {columnName(column)}
                </div>
              </span>
            ))}
          </div>
          {frozenBlock.length > 0 && <div className="hx-cells__frozen">{frozenBlock}</div>}
          <div className="hx-cells__body">{bodyRows}</div>
        </div>
      </div>
      <div className="hx-cells__foot">
        <div className="hx-cells__tabs" role="tablist" aria-label={t('next.sheet.sheets')}>
          {file.sheets.map((sheetName, at) => (
            <button
              key={sheetName}
              type="button"
              role="tab"
              aria-selected={at === file.sheet}
              className="hx-cells__tab"
              onClick={() => {
                setSelection(START);
                undo.current = [];
                scroller.current?.scrollTo(0, 0);
                void file.openSheet(at);
              }}
            >
              {sheetName}
            </button>
          ))}
        </div>
        {stats && many && (
          <span className="hx-cells__stats">
            {t('next.sheet.sum')} {plainNumber(stats.sum)} · {t('next.sheet.average')}{' '}
            {plainNumber(stats.average)}
          </span>
        )}
      </div>
    </div>
  );
};

export default SheetView;
