// The pieces of an annotation that the floating card (annotate.tsx) and the card of a block on a design's page share:
// the soft plate with the words annotated, the field that grows with its text, and the round arrow that sends.
import { useImperativeHandle, useLayoutEffect, useRef, type KeyboardEvent, type Ref } from 'react';
import Icon from '@/next/ui/icon';
import '@/next/styles/annotate.css';

export const clipQuote = (text: string, max: number) =>
  text.length > max ? `${text.slice(0, max - 1).trim()}…` : text;

/** The words annotated: a soft plate with an accent edge, one or two lines, the rest cut off. */
export const AnnotateQuote = ({ text, lead }: { text: string; lead?: string }) => (
  <div className="hx-annot__quote" title={text}>
    {lead && <span className="hx-annot__lead">{lead}</span>}
    <span className="hx-annot__words">{text}</span>
  </div>
);

interface IAnnotateFieldProps {
  value?: string;
  onValue?: (value: string) => void;
  placeholder: string;
  /** Enter without Shift: the note is done. */
  onEnter: () => void;
  onEscape: () => void;
  inputRef?: Ref<HTMLTextAreaElement>;
  className?: string;
}

/** The field of the remark: it grows with its text up to a few lines, Enter ends it, Shift+Enter breaks the line. */
export const AnnotateField = ({
  value,
  onValue,
  placeholder,
  onEnter,
  onEscape,
  inputRef,
  className,
}: IAnnotateFieldProps) => {
  const own = useRef<HTMLTextAreaElement>(null);
  useImperativeHandle(inputRef, () => own.current as HTMLTextAreaElement);
  const grow = () => {
    const field = own.current;
    if (!field) return;
    field.style.height = '0px';
    field.style.height = `${field.scrollHeight}px`;
  };
  useLayoutEffect(grow, [value]);
  const keys = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onEscape();
      return;
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      onEnter();
    }
  };
  return (
    <textarea
      ref={own}
      className={className ? `hx-annot__field ${className}` : 'hx-annot__field'}
      rows={1}
      placeholder={placeholder}
      value={value}
      onChange={onValue ? (event) => onValue(event.target.value) : undefined}
      onInput={value === undefined ? grow : undefined}
      onKeyDown={keys}
    />
  );
};

interface IAnnotateSendProps {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  tabIndex?: number;
  className?: string;
}

/** The round arrow that puts the annotation to the message. */
export const AnnotateSend = ({ label, onClick, disabled, tabIndex, className }: IAnnotateSendProps) => (
  <button
    type="button"
    className={className ? `hx-annot__send ${className}` : 'hx-annot__send'}
    aria-label={label}
    disabled={disabled}
    tabIndex={tabIndex}
    onClick={onClick}
  >
    <Icon name="arrow-up" />
  </button>
);
