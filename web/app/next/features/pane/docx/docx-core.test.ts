// @vitest-environment jsdom
import { strFromU8, strToU8, unzipSync, zipSync } from 'fflate';
import { describe, expect, it } from 'vitest';
import {
  docxSnapshot,
  joinToPrevious,
  mergeDocx,
  paragraphText,
  readDocx,
  replaceRange,
  setParagraphText,
  splitParagraph,
  writeDocx,
  type IDocxParagraph,
  type IDocxTable,
} from '@/next/features/pane/docx/docx-core';

const NS =
  'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"';

const DOC = `<?xml version="1.0"?><w:document ${NS}><w:body>
<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Договор</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>Предмет</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Обычный </w:t></w:r><w:r><w:rPr><w:b/><w:color w:val="C00000"/></w:rPr><w:t>жирный</w:t></w:r><w:r><w:t xml:space="preserve"> конец</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Один</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Два</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr><w:jc w:val="center"/></w:pPr><w:r><w:t>Пункт</w:t></w:r></w:p>
<w:p><w:hyperlink r:id="rId5"><w:r><w:t>ссылка</w:t></w:r></w:hyperlink><w:hyperlink r:id="rId6"><w:r><w:t>плохая</w:t></w:r></w:hyperlink></w:p>
<w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/><w:shd w:fill="FFFF00"/></w:tcPr><w:p><w:r><w:t>Ячейка</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
<w:p><w:del><w:r><w:delText>убрано</w:delText></w:r></w:del><w:ins><w:r><w:t>вставлено</w:t></w:r></w:ins></w:p>
<w:sectPr/></w:body></w:document>`;

const STYLES = `<w:styles ${NS}><w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="200"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:sz w:val="52"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="H1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:rPr><w:b/></w:rPr></w:style></w:styles>`;

const NUMBERING = `<w:numbering ${NS}>
<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:pPr><w:ind w:left="720"/></w:pPr></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val=""/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num></w:numbering>`;

const RELS = `<Relationships><Relationship Id="rId5" Target="https://example.com" TargetMode="External"/><Relationship Id="rId6" Target="javascript:alert(1)" TargetMode="External"/></Relationships>`;

const file = () =>
  zipSync({
    'word/document.xml': strToU8(DOC),
    'word/styles.xml': strToU8(STYLES),
    'word/numbering.xml': strToU8(NUMBERING),
    'word/_rels/document.xml.rels': strToU8(RELS),
    'word/media/keep.bin': new Uint8Array([1, 2, 250]),
  });

const paragraph = (blocks: readonly unknown[], at: number) => blocks[at] as IDocxParagraph;

describe('reading a Word file', () => {
  it('reads headings, runs and looks', () => {
    const { blocks, looks } = readDocx(file());
    expect(paragraph(blocks, 0).heading).toBe(-1);
    expect(paragraph(blocks, 1).heading).toBe(1);
    const runs = paragraph(blocks, 2).runs;
    expect(runs.map((run) => (run.kind === 'text' ? run.text : ''))).toEqual([
      'Обычный ',
      'жирный',
      ' конец',
    ]);
    const bold = runs[1].kind === 'text' ? looks[runs[1].look] : null;
    expect(bold).toMatchObject({ bold: true, color: 'c00000', size: 11 });
  });

  it('counts its lists and aligns', () => {
    const { blocks } = readDocx(file());
    expect(paragraph(blocks, 3).marker).toBe('1.');
    expect(paragraph(blocks, 4).marker).toBe('2.');
    expect(paragraph(blocks, 3).indent).toBe(48);
    expect(paragraph(blocks, 5).marker).toBe('•');
    expect(paragraph(blocks, 5).align).toBe('center');
    expect(paragraph(blocks, 2).after).toBeCloseTo(13.33, 1);
  });

  it('lets only a real address out as a link', () => {
    const runs = paragraph(readDocx(file()).blocks, 6).runs;
    expect(runs[0]).toMatchObject({ href: 'https://example.com' });
    expect(runs[1]).toMatchObject({ href: null });
  });

  it('reads a table and leaves out what was taken out in tracked changes', () => {
    const book = readDocx(file());
    const table = book.blocks[7] as IDocxTable;
    expect(table.rows[0][0]).toMatchObject({ span: 2, fill: 'ffff00' });
    expect(paragraphText(book, 8)).toBe('вставлено');
  });
});

describe('editing a Word file', () => {
  it('changes only the words edited, and the rest of the file stays', () => {
    const book = readDocx(file());
    expect(setParagraphText(book, 2, 'Обычный жирный новый конец')).toBe(true);
    const saved = unzipSync(writeDocx(book));
    expect([...saved['word/media/keep.bin']]).toEqual([1, 2, 250]);
    expect(strFromU8(saved['word/styles.xml'])).toBe(STYLES);
    const again = readDocx(writeDocx(book));
    expect(paragraphText(again, 2)).toBe('Обычный жирный новый конец');
    const runs = paragraph(again.blocks, 2).runs;
    expect(runs[1]).toMatchObject({ text: 'жирный' });
    expect(again.looks[runs[1].kind === 'text' ? runs[1].look : 0].bold).toBe(true);
  });

  it('tells when nothing changed', () => {
    const book = readDocx(file());
    expect(setParagraphText(book, 1, 'Предмет')).toBe(false);
  });

  it('splits a paragraph at the caret, in the same look', () => {
    const book = readDocx(file());
    splitParagraph(book, 3, 2);
    const again = readDocx(writeDocx(book));
    expect(paragraphText(again, 3)).toBe('Од');
    expect(paragraphText(again, 4)).toBe('ин');
    expect(paragraph(again.blocks, 4).marker).toBe('2.');
    expect(paragraphText(again, 5)).toBe('Два');
  });

  it('joins a paragraph to the one before it', () => {
    const book = readDocx(file());
    expect(joinToPrevious(book, 4)).toBe(4);
    const again = readDocx(writeDocx(book));
    expect(paragraphText(again, 3)).toBe('ОдинДва');
    expect(paragraphText(again, 4)).toBe('Пункт');
  });

  it('will not join across a table', () => {
    const book = readDocx(file());
    expect(joinToPrevious(book, 8)).toBeNull();
  });

  it('leaves a tab and a mark on their own side of a split', () => {
    const body = `<w:p><w:bookmarkStart w:id="1"/><w:r><w:rPr><w:b/></w:rPr><w:t>Имя</w:t><w:tab/><w:t>Значение</w:t></w:r></w:p>`;
    const book = readDocx(
      zipSync({ 'word/document.xml': strToU8(`<w:document ${NS}><w:body>${body}</w:body></w:document>`) }),
    );
    splitParagraph(book, 0, 5);
    const again = readDocx(writeDocx(book));
    expect(paragraphText(again, 0)).toBe('ИмяЗн');
    expect(paragraphText(again, 1)).toBe('ачение');
    const xml = strFromU8(unzipSync(writeDocx(book))['word/document.xml']);
    expect(xml.match(/<w:tab\/>/g)).toHaveLength(1);
    expect(xml.match(/bookmarkStart/g)).toHaveLength(1);
    expect(xml.match(/<w:b\/>/g)).toHaveLength(2);
  });

  it('keeps the look for the words to come after a split at the end', () => {
    const book = readDocx(file());
    splitParagraph(book, 2, paragraphText(book, 2).length);
    setParagraphText(book, 3, 'дальше');
    expect(paragraphText(book, 2)).toBe('Обычный жирный конец');
    expect(paragraphText(readDocx(writeDocx(book)), 3)).toBe('дальше');
  });

  it('replaces a stretch across paragraphs', () => {
    const book = readDocx(file());
    const caret = replaceRange(book, { index: 3, offset: 2 }, { index: 4, offset: 1 }, 'X');
    expect(caret).toEqual({ index: 3, offset: 3 });
    expect(paragraphText(book, 3)).toBe('ОдXва');
    expect(paragraphText(book, 4)).toBe('Пункт');
  });

  it('turns line breaks into paragraphs', () => {
    const book = readDocx(file());
    const caret = replaceRange(book, { index: 3, offset: 2 }, { index: 3, offset: 2 }, 'а\nб\nв');
    expect(caret).toEqual({ index: 5, offset: 1 });
    expect(docxSnapshot(book).slice(3, 7)).toEqual(['Ода', 'б', 'вин', 'Два']);
  });

  it('will not replace across a table', () => {
    const book = readDocx(file());
    expect(replaceRange(book, { index: 6, offset: 0 }, { index: 8, offset: 0 }, '')).toBeNull();
    expect(paragraphText(book, 6)).toBe('ссылкаплохая');
  });

  it('types into a paragraph that was empty', () => {
    const empty = zipSync({
      'word/document.xml': strToU8(`<w:document ${NS}><w:body><w:p/></w:body></w:document>`),
    });
    const book = readDocx(empty);
    setParagraphText(book, 0, 'слова');
    expect(paragraphText(readDocx(writeDocx(book)), 0)).toBe('слова');
  });
});

describe('a file that changed under the edits', () => {
  it('takes the edits on top, paragraph by paragraph', () => {
    const base = docxSnapshot(readDocx(file()));
    const mine = readDocx(file());
    const theirs = readDocx(file());
    setParagraphText(mine, 1, 'Мой предмет');
    setParagraphText(theirs, 3, 'Их один');
    expect(mergeDocx(base, mine, theirs)).toBe(true);
    expect(paragraphText(theirs, 1)).toBe('Мой предмет');
    expect(paragraphText(theirs, 3)).toBe('Их один');
  });

  it('gives up when both changed the same paragraph', () => {
    const base = docxSnapshot(readDocx(file()));
    const mine = readDocx(file());
    const theirs = readDocx(file());
    setParagraphText(mine, 1, 'Мой');
    setParagraphText(theirs, 1, 'Их');
    expect(mergeDocx(base, mine, theirs)).toBe(false);
  });

  it('gives up when paragraphs were added', () => {
    const base = docxSnapshot(readDocx(file()));
    const mine = readDocx(file());
    const theirs = readDocx(file());
    setParagraphText(mine, 1, 'Мой');
    splitParagraph(theirs, 3, 1);
    expect(mergeDocx(base, mine, theirs)).toBe(false);
  });
});
