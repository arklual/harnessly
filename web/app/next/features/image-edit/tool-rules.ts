// The editor's tools and what each needs before «send» is on (the rules, away from the screen).
import { EDIT_MODE, type TEditFormat, type TEditMode } from '@/next/features/image-edit/host';
import { boxCounts, type IMarks } from '@/next/features/image-edit/marks';

export const TOOL = {
  mark: 'mark',
  note: 'note',
  cut: 'cut',
  erase: 'erase',
  size: 'size',
} as const;
export type TTool = (typeof TOOL)[keyof typeof TOOL];

export const TOOLS = [
  { id: TOOL.mark, icon: 'pencil' },
  { id: TOOL.note, icon: 'message-square-text' },
  { id: TOOL.cut, icon: 'image' },
  { id: TOOL.erase, icon: 'eraser' },
  { id: TOOL.size, icon: 'maximize-2' },
] as const;

/** The tools that draw on the picture: the page does not scroll or zoom under a finger, zoom and swiping are off. */
export const DRAWING_TOOLS: readonly TTool[] = [TOOL.mark, TOOL.note, TOOL.erase];
export const isDrawing = (tool: TTool | null): boolean => tool !== null && DRAWING_TOOLS.includes(tool);

/** The mode a tool starts; no tool: the words alone. */
export const modeOf = (tool: TTool | null): TEditMode => (tool === null ? EDIT_MODE.text : EDIT_MODE[tool]);

export const DEFAULT_FORMAT: TEditFormat = '16:9';

export const ratioOfFormat = (format: TEditFormat): number => {
  const [w, h] = format.split(':').map(Number);
  return w / h;
};

export interface ISendState {
  tool: TTool | null;
  marks: IMarks;
  text: string;
  running: boolean;
}

/** Words are optional for `mark`, `erase` and `size`, required for `note` and for no tool; `cut` runs by itself. */
export const canSend = ({ tool, marks, text, running }: ISendState): boolean => {
  if (running) return false;
  const words = text.trim() !== '';
  switch (tool) {
    case TOOL.mark:
    case TOOL.erase:
      return marks.strokes.length > 0;
    case TOOL.note:
      return boxCounts(marks.box) && words;
    case TOOL.size:
      return true;
    case TOOL.cut:
      return false;
    case null:
      return words;
  }
};
