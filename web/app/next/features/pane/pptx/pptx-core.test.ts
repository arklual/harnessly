// @vitest-environment jsdom
import { strFromU8, strToU8, unzipSync, zipSync } from 'fflate';
import { describe, expect, it } from 'vitest';
import {
  drawnSlide,
  mergePptx,
  pptxParagraphs,
  pptxSnapshot,
  readPptx,
  replacePptxRange,
  slideTitle,
  writePptx,
  type TPptxShape,
} from '@/next/features/pane/pptx/pptx-core';

const NS =
  'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"';
const T = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const rels = (items: string) => strToU8(`<Relationships>${items}</Relationships>`);
const rel = (id: string, type: string, target: string, external = false) =>
  `<Relationship Id="${id}" Type="${T}/${type}" Target="${target}"${external ? ' TargetMode="External"' : ''}/>`;

const SLIDE1 = `<p:sld ${NS}><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title 1"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:r><a:t>Заголовок</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:cNvPr id="3" name="Body"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:r><a:rPr b="1"><a:solidFill><a:schemeClr val="accent1"><a:lumMod val="50000"/></a:schemeClr></a:solidFill></a:rPr><a:t>Жирный</a:t></a:r><a:r><a:t> обычный</a:t></a:r></a:p>
<a:p><a:r><a:rPr><a:hlinkClick r:id="rId5"/></a:rPr><a:t>ссылка</a:t></a:r><a:endParaRPr sz="1000"/></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:cNvPr id="4" name="Box"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
<p:spPr><a:xfrm rot="5400000"><a:off x="952500" y="1905000"/><a:ext cx="1905000" cy="952500"/></a:xfrm><a:prstGeom prst="roundRect"/><a:solidFill><a:srgbClr val="FF8800"><a:alpha val="50000"/></a:srgbClr></a:solidFill><a:ln w="19050"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:ln></p:spPr>
<p:txBody><a:bodyPr anchor="ctr"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr sz="1200"/><a:t>В фигуре</a:t></a:r></a:p></p:txBody></p:sp>
<p:pic><p:nvPicPr><p:cNvPr id="5" name="Photo"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rId6"/><a:srcRect l="10000" r="10000"/></p:blipFill><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="9525" cy="9525"/></a:xfrm></p:spPr></p:pic>
<p:grpSp><p:grpSpPr><a:xfrm><a:off x="952500" y="0"/><a:ext cx="952500" cy="952500"/><a:chOff x="0" y="0"/><a:chExt cx="1905000" cy="1905000"/></a:xfrm></p:grpSpPr>
<p:sp><p:nvSpPr><p:cNvPr id="6" name="In group"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="952500" y="952500"/><a:ext cx="952500" cy="952500"/></a:xfrm><a:prstGeom prst="ellipse"/></p:spPr></p:sp></p:grpSp>
<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="7" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="0" y="0"/><a:ext cx="952500" cy="952500"/></p:xfrm><a:graphic><a:graphicData uri="x/table"><a:tbl><a:tblGrid><a:gridCol w="952500"/></a:tblGrid><a:tr h="476250"><a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:t>Ячейка</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></a:tcPr></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>
<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="8" name="Chart"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="0" y="0"/><a:ext cx="9525" cy="9525"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"/></a:graphic></p:graphicFrame>
</p:spTree></p:cSld></p:sld>`;

const SLIDE2 = `<p:sld ${NS}><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="112233"/></a:solidFill></p:bgPr></p:bg><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:t>Второй</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`;

const LAYOUT = `<p:sldLayout ${NS}><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="95250" y="190500"/><a:ext cx="952500" cy="476250"/></a:xfrm></p:spPr><p:txBody><a:bodyPr anchor="b"/><a:lstStyle><a:lvl1pPr><a:defRPr sz="4000"/></a:lvl1pPr></a:lstStyle><a:p><a:r><a:t>Подсказка макета</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:cNvPr id="3" name="Body"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="95250" y="952500"/><a:ext cx="952500" cy="952500"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:t>Подсказка</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sldLayout>`;

const MASTER = `<p:sldMaster ${NS}><p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Logo"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="95250" cy="95250"/></a:xfrm><a:prstGeom prst="rect"/><a:solidFill><a:schemeClr val="accent1"/></a:solidFill></p:spPr></p:sp>
</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1"/>
<p:txStyles><p:titleStyle><a:lvl1pPr algn="ctr"><a:defRPr sz="4400" b="1"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle>
<p:bodyStyle><a:lvl1pPr marL="285750" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="•"/><a:defRPr sz="2800"/></a:lvl1pPr></p:bodyStyle></p:txStyles></p:sldMaster>`;

const THEME = `<a:theme ${NS}><a:themeElements><a:clrScheme name="x"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="4080C0"/></a:accent1></a:clrScheme><a:fontScheme name="x"><a:majorFont><a:latin typeface="Georgia"/></a:majorFont><a:minorFont><a:latin typeface="Arial"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`;

const file = () =>
  zipSync({
    'ppt/presentation.xml': strToU8(
      `<p:presentation ${NS}><p:sldIdLst><p:sldId id="256" r:id="rId1"/><p:sldId id="257" r:id="rId2"/></p:sldIdLst><p:sldSz cx="9525000" cy="5715000"/></p:presentation>`,
    ),
    'ppt/_rels/presentation.xml.rels': rels(
      rel('rId1', 'slide', 'slides/slide1.xml') + rel('rId2', 'slide', 'slides/slide2.xml'),
    ),
    'ppt/slides/slide1.xml': strToU8(SLIDE1),
    'ppt/slides/_rels/slide1.xml.rels': rels(
      rel('rId1', 'slideLayout', '../slideLayouts/slideLayout1.xml') +
        rel('rId5', 'hyperlink', 'https://example.com', true) +
        rel('rId6', 'image', '../media/image1.png'),
    ),
    'ppt/slides/slide2.xml': strToU8(SLIDE2),
    'ppt/slides/_rels/slide2.xml.rels': rels(rel('rId1', 'slideLayout', '../slideLayouts/slideLayout1.xml')),
    'ppt/slideLayouts/slideLayout1.xml': strToU8(LAYOUT),
    'ppt/slideLayouts/_rels/slideLayout1.xml.rels': rels(
      rel('rId1', 'slideMaster', '../slideMasters/slideMaster1.xml'),
    ),
    'ppt/slideMasters/slideMaster1.xml': strToU8(MASTER),
    'ppt/slideMasters/_rels/slideMaster1.xml.rels': rels(rel('rId1', 'theme', '../theme/theme1.xml')),
    'ppt/theme/theme1.xml': strToU8(THEME),
    'ppt/media/image1.png': new Uint8Array([1, 2, 250]),
  });

const named = (shapes: readonly TPptxShape[], name: string) => {
  const shape = shapes.find((item) => item.name === name);
  if (!shape) throw new Error(`no ${name}`);
  return shape;
};

describe('reading a PowerPoint file', () => {
  it('knows its size and its slides', () => {
    const book = readPptx(file());
    expect(book.width).toBe(1000);
    expect(book.height).toBe(600);
    expect(book.slides).toHaveLength(2);
    expect(slideTitle(book.slides[0])).toBe('Заголовок');
  });

  it('takes a placeholder’s place and look from its layout and master', () => {
    const { shapes } = drawnSlide(readPptx(file()), 0);
    const title = named(shapes, 'Title 1');
    if (title.kind !== 'shape' || !title.text) throw new Error('not text');
    expect(title.box).toMatchObject({ x: 10, y: 20, w: 100, h: 50 });
    expect(title.text.anchor).toBe('bottom');
    const paragraph = title.text.paragraphs[0];
    expect(paragraph.align).toBe('center');
    const run = paragraph.runs[0];
    if (run.kind !== 'text') throw new Error('not text');
    expect(run.size).toBeCloseTo((40 * 4) / 3);
    expect(run.bold).toBe(true);
    expect(run.color).toBe('#000000');
    expect(run.font).toBe('Georgia');
  });

  it('gives bullets, colours with their changes, and links', () => {
    const { shapes } = drawnSlide(readPptx(file()), 0);
    const body = named(shapes, 'Body');
    if (body.kind !== 'shape' || !body.text) throw new Error('not text');
    const first = body.text.paragraphs[0];
    expect(first.marker).toBe('•');
    expect(first.left).toBe(30);
    expect(first.firstLine).toBe(-30);
    const run = first.runs[0];
    if (run.kind !== 'text') throw new Error('not text');
    expect(run.bold).toBe(true);
    expect(run.color).toBe('#204060');
    const link = body.text.paragraphs[1].runs[0];
    expect(link).toMatchObject({ href: 'https://example.com' });
  });

  it('reads a shape, a picture, a group and a table', () => {
    const { shapes } = drawnSlide(readPptx(file()), 0);
    expect(named(shapes, 'Box')).toMatchObject({
      geometry: 'roundRect',
      fill: { kind: 'solid', color: 'rgb(255 136 0 / 50%)' },
      line: { color: '#000000', width: 2 },
      box: { rotation: 90 },
    });
    expect(named(shapes, 'Photo')).toMatchObject({ path: 'ppt/media/image1.png', crop: { l: 0.1, r: 0.1 } });
    expect(named(shapes, 'In group').box).toMatchObject({ x: 150, y: 50, w: 50, h: 50 });
    expect(named(shapes, 'Table')).toMatchObject({
      columns: [100],
      rows: [{ height: 50, cells: [{ fill: { kind: 'solid', color: '#00ff00' } }] }],
    });
    expect(named(shapes, 'Chart')).toMatchObject({ kind: 'other', what: 'chart' });
  });

  it('draws what stands on the master, and no prompt of a layout', () => {
    const { shapes } = drawnSlide(readPptx(file()), 1);
    expect(named(shapes, 'Logo')).toMatchObject({ fill: { kind: 'solid', color: '#4080c0' } });
    const words = JSON.stringify(shapes);
    expect(words).not.toContain('Подсказка');
    expect(words).toContain('Второй');
  });

  it('takes the background from the slide or from what stands behind it', () => {
    const book = readPptx(file());
    expect(drawnSlide(book, 1).background).toEqual({ kind: 'solid', color: '#112233' });
    expect(drawnSlide(book, 0).background).toEqual({ kind: 'solid', color: '#ffffff' });
  });
});

describe('editing a PowerPoint file', () => {
  it('changes only the words edited and only the slide they are on', () => {
    const book = readPptx(file());
    const body = pptxParagraphs(book, 0);
    expect(body.setText(1, 'Жирный новый обычный')).toBe(true);
    const saved = writePptx(book);
    const parts = unzipSync(saved);
    expect([...parts['ppt/media/image1.png']]).toEqual([1, 2, 250]);
    expect(strFromU8(parts['ppt/slides/slide2.xml'])).toBe(SLIDE2);
    const again = readPptx(saved);
    expect(pptxParagraphs(again, 0).text(1)).toBe('Жирный новый обычный');
    const shape = named(drawnSlide(again, 0).shapes, 'Body');
    if (shape.kind !== 'shape' || !shape.text) throw new Error('not text');
    expect(shape.text.paragraphs[0].runs[0]).toMatchObject({ text: 'Жирный', bold: true });
    expect(shape.text.paragraphs[0].runs[1]).toMatchObject({ text: ' новый обычный', bold: false });
  });

  it('splits and joins paragraphs in a shape, closing mark last', () => {
    const book = readPptx(file());
    const caret = replacePptxRange(book, 0, { index: 1, offset: 3 }, { index: 1, offset: 3 }, '\n');
    expect(caret).toEqual({ index: 2, offset: 0 });
    const body = pptxParagraphs(book, 0);
    expect(body.text(1)).toBe('Жир');
    expect(body.text(2)).toBe('ный обычный');
    expect(body.join(2)).toBe(3);
    expect(body.text(1)).toBe('Жирный обычный');
    expect(body.text(2)).toBe('ссылка');
    const paragraph = book.slides[0].paragraphs[2];
    expect(paragraph.lastElementChild?.localName).toBe('endParaRPr');
    expect(pptxParagraphs(readPptx(writePptx(book)), 0).text(2)).toBe('ссылка');
  });

  it('types into a paragraph with no words, in the look of its closing mark', () => {
    const empty = `<p:sld ${NS}><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2" name="X"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="9525" cy="9525"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:endParaRPr sz="3000"/></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`;
    const parts = unzipSync(file());
    const book = readPptx(zipSync({ ...parts, 'ppt/slides/slide1.xml': strToU8(empty) }));
    pptxParagraphs(book, 0).setText(0, 'слова');
    const shape = named(drawnSlide(book, 0).shapes, 'X');
    if (shape.kind !== 'shape' || !shape.text) throw new Error('not text');
    expect(shape.text.paragraphs[0].runs[0]).toMatchObject({ text: 'слова', size: 40 });
    expect(book.slides[0].paragraphs[0].lastElementChild?.localName).toBe('endParaRPr');
  });

  it('will not join across shapes', () => {
    const book = readPptx(file());
    expect(pptxParagraphs(book, 0).join(1)).toBeNull();
  });

  it('takes the edits on top of a file that changed', () => {
    const base = pptxSnapshot(readPptx(file()));
    const mine = readPptx(file());
    const theirs = readPptx(file());
    pptxParagraphs(mine, 0).setText(0, 'Мой');
    pptxParagraphs(theirs, 1).setText(0, 'Их');
    expect(mergePptx(base, mine, theirs)).toBe(true);
    expect(pptxParagraphs(theirs, 0).text(0)).toBe('Мой');
    expect(pptxParagraphs(theirs, 1).text(0)).toBe('Их');
    expect(theirs.changed.size).toBe(2);
  });

  it('gives up when both changed one paragraph', () => {
    const base = pptxSnapshot(readPptx(file()));
    const mine = readPptx(file());
    const theirs = readPptx(file());
    pptxParagraphs(mine, 0).setText(0, 'Мой');
    pptxParagraphs(theirs, 0).setText(0, 'Их');
    expect(mergePptx(base, mine, theirs)).toBe(false);
  });
});
