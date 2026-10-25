// The editor's engine settings: CodeMirror 6 with what an IDE's text editor has — line numbers, the active line,
// brackets, folding, search, history, indentation by the language, completions — in the app's own tokens.
import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completeAnyWord,
} from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  indentOnInput,
  indentUnit,
  syntaxHighlighting,
} from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { Compartment, EditorSelection, EditorState, type Extension } from '@codemirror/state';
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  type Command,
  type ViewUpdate,
} from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { markdownHints } from '@/next/features/pane/editor/hints';
import { t } from '@/next/i18n';

/** The parts that change after the editor is made: the language arrives later, a markdown file wraps its lines. */
export const language = new Compartment();
export const wrapping = new Compartment();

const syn = (name: string) => `var(--hx-syn-${name})`;

const highlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.operatorKeyword, tags.modifier, tags.self], color: syn('keyword') },
  { tag: [tags.string, tags.special(tags.string), tags.inserted], color: syn('string') },
  { tag: [tags.number, tags.bool, tags.null, tags.atom, tags.color], color: syn('number') },
  { tag: [tags.comment, tags.meta], color: syn('comment'), fontStyle: 'italic' },
  {
    tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.labelName],
    color: syn('func'),
  },
  { tag: [tags.typeName, tags.className, tags.namespace, tags.annotation], color: syn('type') },
  { tag: [tags.propertyName, tags.attributeName], color: syn('prop') },
  { tag: [tags.tagName, tags.standard(tags.tagName)], color: syn('tag') },
  { tag: [tags.operator, tags.punctuation, tags.regexp, tags.escape], color: syn('op') },
  { tag: [tags.link, tags.url], color: syn('link') },
  { tag: tags.deleted, color: 'var(--hx-danger)' },
  { tag: tags.invalid, color: 'var(--hx-danger)' },
  // Markdown, written the way it will read.
  { tag: tags.heading1, fontSize: '1.7em', fontWeight: '700' },
  { tag: tags.heading2, fontSize: '1.4em', fontWeight: '700' },
  { tag: tags.heading3, fontSize: '1.2em', fontWeight: '650' },
  { tag: [tags.heading4, tags.heading5, tags.heading6], fontWeight: '650' },
  { tag: tags.strong, fontWeight: '700' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.strikethrough, textDecoration: 'line-through', color: syn('comment') },
  { tag: tags.link, textDecoration: 'underline' },
  { tag: tags.quote, color: 'var(--hx-fg-2)', fontStyle: 'italic' },
  { tag: tags.monospace, fontFamily: 'var(--hx-font-mono)', fontSize: '0.92em', color: syn('string') },
  { tag: tags.processingInstruction, color: 'var(--hx-fg-3)', fontWeight: '400' },
  { tag: tags.contentSeparator, color: 'var(--hx-fg-3)' },
]);

const theme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'transparent', color: 'var(--hx-fg-1)', fontSize: '13px' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': { fontFamily: 'var(--hx-font-mono)', lineHeight: '1.65', overflow: 'auto' },
  '.cm-content': { padding: '8px 0 64px', caretColor: 'var(--hx-accent)' },
  '.cm-line': { padding: '0 16px 0 8px' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--hx-accent)', borderLeftWidth: '2px' },
  '.cm-selectionBackground': { backgroundColor: 'var(--hx-accent-soft)' },
  '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground': {
    backgroundColor: 'var(--hx-accent-soft)',
  },
  '.cm-activeLine': { backgroundColor: 'var(--hx-bg-hover)' },
  '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--hx-fg-3)', border: 'none' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--hx-fg-1)' },
  '.cm-lineNumbers .cm-gutterElement': { minWidth: '28px', padding: '0 6px 0 12px', fontSize: '12px' },
  '.cm-foldGutter .cm-gutterElement': { padding: '0 4px', cursor: 'pointer' },
  '.cm-foldPlaceholder': {
    padding: '0 6px',
    border: '1px solid var(--hx-border-strong)',
    borderRadius: '6px',
    backgroundColor: 'var(--hx-bg-active)',
    color: 'var(--hx-fg-2)',
  },
  '&.cm-focused .cm-matchingBracket': {
    backgroundColor: 'var(--hx-accent-soft)',
    outline: '1px solid var(--hx-border-strong)',
  },
  '&.cm-focused .cm-nonmatchingBracket': { backgroundColor: 'transparent', color: 'var(--hx-danger)' },
  '.cm-selectionMatch': { backgroundColor: 'var(--hx-bg-active)' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--hx-warning) 26%, transparent)' },
  '.cm-searchMatch.cm-searchMatch-selected': {
    backgroundColor: 'color-mix(in srgb, var(--hx-warning) 52%, transparent)',
  },
  '.cm-panels': { backgroundColor: 'var(--hx-bg-raised)', color: 'var(--hx-fg-1)' },
  '.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--hx-border)' },
  '.cm-panel.cm-search': {
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: '6px',
    padding: '8px 12px',
  },
  '.cm-panel.cm-search br': { display: 'none' },
  '.cm-panel.cm-search input.cm-textfield': {
    height: '28px',
    padding: '0 10px',
    border: '1px solid var(--hx-border-strong)',
    borderRadius: '8px',
    backgroundColor: 'var(--hx-bg-app)',
    color: 'var(--hx-fg-1)',
    font: 'inherit',
    fontSize: '13px',
    margin: '0',
  },
  '.cm-panel.cm-search input.cm-textfield:focus': {
    outline: '2px solid var(--hx-focus)',
    outlineOffset: '0',
  },
  '.cm-panel.cm-search button.cm-button': {
    height: '28px',
    padding: '0 10px',
    border: '1px solid var(--hx-border-strong)',
    borderRadius: '8px',
    backgroundImage: 'none',
    backgroundColor: 'transparent',
    color: 'var(--hx-fg-1)',
    font: 'inherit',
    fontSize: '12.5px',
    cursor: 'pointer',
    margin: '0',
  },
  '.cm-panel.cm-search button.cm-button:hover': { backgroundColor: 'var(--hx-bg-hover)' },
  '.cm-panel.cm-search label': {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    fontSize: '12.5px',
    color: 'var(--hx-fg-2)',
  },
  '.cm-panel.cm-search [name=close]': {
    position: 'static',
    marginLeft: 'auto',
    border: '0',
    background: 'transparent',
    color: 'var(--hx-fg-2)',
    fontSize: '18px',
    cursor: 'pointer',
  },
  '.cm-tooltip': {
    border: '1px solid var(--hx-border-strong)',
    borderRadius: '12px',
    backgroundColor: 'var(--hx-bg-raised)',
    boxShadow: 'var(--hx-shadow-overlay)',
    overflow: 'hidden',
  },
  '.cm-tooltip-autocomplete > ul': {
    fontFamily: 'var(--hx-font-mono)',
    fontSize: '12.5px',
    maxHeight: '240px',
  },
  '.cm-tooltip-autocomplete > ul > li': { padding: '3px 10px', color: 'var(--hx-fg-1)' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    backgroundColor: 'var(--hx-accent-soft)',
    color: 'var(--hx-fg-1)',
  },
  '.cm-completionDetail': {
    marginLeft: '12px',
    color: 'var(--hx-fg-3)',
    fontFamily: 'var(--hx-font-ui)',
    fontStyle: 'normal',
  },
  '.cm-completionMatchedText': { textDecoration: 'none', color: 'var(--hx-accent)', fontWeight: '600' },
});

/** The search panel's words (CodeMirror ships English ones), in the page's language. */
const PHRASES = [
  ['Find', 'find'],
  ['Replace', 'replace'],
  ['next', 'next'],
  ['previous', 'previous'],
  ['all', 'all'],
  ['match case', 'matchCase'],
  ['regexp', 'regexp'],
  ['by word', 'byWord'],
  ['replace', 'replaceOne'],
  ['replace all', 'replaceAll'],
  ['close', 'close'],
  ['Go to line', 'goToLine'],
  ['go', 'go'],
] as const;

const phrases = () =>
  EditorState.phrases.of(
    Object.fromEntries(PHRASES.map(([from, key]) => [from, t(`next.editor.phrase.${key}`)])),
  );

/** Wraps the selection in a mark, or takes the mark off when it is already there. */
const wrapIn =
  (mark: string): Command =>
  (view) => {
    view.dispatch(
      view.state.changeByRange((range) => {
        const { from, to } = range;
        const around = view.state.sliceDoc(from - mark.length, to + mark.length);
        if (around.startsWith(mark) && around.endsWith(mark) && around.length >= mark.length * 2)
          return {
            changes: [
              { from: from - mark.length, to: from },
              { from: to, to: to + mark.length },
            ],
            range: EditorSelection.range(from - mark.length, to - mark.length),
          };
        return {
          changes: [
            { from, insert: mark },
            { from: to, insert: mark },
          ],
          range: EditorSelection.range(from + mark.length, to + mark.length),
        };
      }),
    );
    return true;
  };

/** Tabs when most indented lines of the file begin with one; two spaces otherwise. */
export const indentationOf = (text: string): Extension => {
  const tabs = text.match(/^\t/gm)?.length ?? 0;
  const spaces = text.match(/^ {2,}/gm)?.length ?? 0;
  return tabs > spaces ? [indentUnit.of('\t'), EditorState.tabSize.of(4)] : indentUnit.of('  ');
};

export interface IEditorOptions {
  eol: '\n' | '\r\n';
  text: string;
  markdown: boolean;
  wrap: boolean;
  onUpdate: (update: ViewUpdate) => void;
}

export const extensions = ({ eol, text, markdown, wrap, onUpdate }: IEditorOptions): Extension[] => [
  EditorState.lineSeparator.of(eol),
  phrases(),
  indentationOf(text),
  lineNumbers(),
  highlightActiveLineGutter(),
  highlightSpecialChars(),
  history(),
  foldGutter(),
  drawSelection(),
  indentOnInput(),
  bracketMatching(),
  closeBrackets(),
  highlightActiveLine(),
  highlightSelectionMatches(),
  search({ top: true }),
  autocompletion({ icons: false }),
  // Words of the document complete in every language, beside what the language itself knows.
  EditorState.languageData.of(() => [{ autocomplete: completeAnyWord }]),
  markdown ? EditorState.languageData.of(() => [{ autocomplete: markdownHints }]) : [],
  markdown
    ? keymap.of([
        { key: 'Mod-b', run: wrapIn('**') },
        { key: 'Mod-i', run: wrapIn('_') },
      ])
    : [],
  keymap.of([
    ...closeBracketsKeymap,
    ...defaultKeymap,
    ...searchKeymap,
    ...historyKeymap,
    ...foldKeymap,
    indentWithTab,
  ]),
  syntaxHighlighting(highlight),
  theme,
  language.of([]),
  wrapping.of(wrap ? EditorView.lineWrapping : []),
  EditorView.contentAttributes.of({ 'aria-label': t('next.editor.label') }),
  EditorView.updateListener.of(onUpdate),
];
