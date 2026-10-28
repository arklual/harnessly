package design

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The PPTX writer: the minimum of an Office Open XML presentation that PowerPoint, Keynote and LibreOffice open. Each
// slide is a full-bleed picture of the board (its text taken out) with the board's texts laid over it as text boxes,
// and its speaker notes in a notes page. One slide master, one blank layout, one theme; no part is left out that an
// application is known to ask for, and none is there that nothing refers to.

// pptxRun is a piece of a paragraph in one style, or a line break. Sizes are the board's pixels.
type pptxRun struct {
	Text      string
	Br        bool
	Size      float64
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	// Color is RRGGBB; Alpha is 0..1 (1 opaque).
	Color   string
	Alpha   float64
	Font    string
	Spacing float64
}

// pptxText is a text box: its place in the board's pixels, the text's alignment ("l", "ctr", "r", "just") and line
// height, and the paragraph's runs. Wrap is true for a text of several lines.
type pptxText struct {
	X, Y, W, H float64
	Align      string
	LineH      float64
	Wrap       bool
	Runs       []pptxRun
}

type pptxSlide struct {
	Title   string
	Picture []byte
	Texts   []pptxText
	Notes   string
}

type pptxDeck struct {
	Title  string
	W, H   int
	Slides []pptxSlide
	Lang   string
}

const (
	pptxWidthEMU  = 12192000
	pptxNSa       = "http://schemas.openxmlformats.org/drawingml/2006/main"
	pptxNSr       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	pptxNSp       = "http://schemas.openxmlformats.org/presentationml/2006/main"
	pptxRelBase   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	pptxXMLHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	pptxMaxBoxes  = 300
)

// pptxType is the content type of a part kind.
const (
	ctMain  = "application/vnd.openxmlformats-officedocument.presentationml."
	ctRels  = "application/vnd.openxmlformats-package.relationships+xml"
	ctTheme = "application/vnd.openxmlformats-officedocument.theme+xml"
	ctCore  = "application/vnd.openxmlformats-package.core-properties+xml"
	ctApp   = "application/vnd.openxmlformats-officedocument.extended-properties+xml"
)

// pptxEsc is a string for XML text or an attribute: the characters XML cannot hold are dropped.
func pptxEsc(s string) string {
	var clean strings.Builder
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || r >= 0x10000 {
			clean.WriteRune(r)
		}
	}
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(clean.String()))
	return strings.ReplaceAll(b.String(), "&#34;", "&quot;")
}

func relsXML(rels [][3]string) string {
	var b strings.Builder
	b.WriteString(pptxXMLHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range rels {
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"/>`, r[0], r[1], r[2])
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func emu(px, scale float64) int { return int(math.Round(px * scale)) }

func (d pptxDeck) heightEMU() int {
	return int(math.Round(float64(pptxWidthEMU) * float64(d.H) / float64(d.W)))
}

func pptxSizeAttr(px, scale float64) string {
	sz := int(math.Round(px * scale / 12700 * 100))
	if sz < 100 {
		sz = 100
	}
	if sz > 400000 {
		sz = 400000
	}
	return strconv.Itoa(sz)
}

func (r pptxRun) props(tag string, scale float64, lang string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<a:%s lang="%s" sz="%s"`, tag, pptxEsc(lang), pptxSizeAttr(r.Size, scale))
	if r.Bold {
		b.WriteString(` b="1"`)
	} else {
		b.WriteString(` b="0"`)
	}
	if r.Italic {
		b.WriteString(` i="1"`)
	} else {
		b.WriteString(` i="0"`)
	}
	if r.Underline {
		b.WriteString(` u="sng"`)
	}
	if r.Strike {
		b.WriteString(` strike="sngStrike"`)
	}
	if r.Spacing != 0 {
		fmt.Fprintf(&b, ` spc="%d"`, int(math.Round(r.Spacing*scale/12700*100)))
	}
	b.WriteString(` dirty="0">`)
	b.WriteString(`<a:solidFill><a:srgbClr val="` + strings.ToUpper(r.Color) + `">`)
	if r.Alpha > 0 && r.Alpha < 0.995 {
		fmt.Fprintf(&b, `<a:alpha val="%d"/>`, int(math.Round(r.Alpha*100000)))
	}
	b.WriteString(`</a:srgbClr></a:solidFill>`)
	font := r.Font
	if font == "" {
		font = "Arial"
	}
	b.WriteString(`<a:latin typeface="` + pptxEsc(font) + `"/><a:cs typeface="` + pptxEsc(font) + `"/>`)
	b.WriteString(`</a:` + tag + `>`)
	return b.String()
}

// alignment is the paragraph's alignment, one of the four the format has; anything else is left.
func (t pptxText) alignment() string {
	switch t.Align {
	case "ctr", "r", "just":
		return t.Align
	}
	return "l"
}

var pptxHexColor = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

func (t pptxText) xml(id int, scale float64, lang string) string {
	var b strings.Builder
	w := t.W
	wrap := "none"
	if t.Wrap {
		wrap = "square"
		w = t.W * 1.05
	}
	x := t.X
	switch t.alignment() {
	case "ctr":
		x -= (w - t.W) / 2
	case "r":
		x -= w - t.W
	}
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>`, id, id-2)
	fmt.Fprintf(&b, `<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>`,
		emu(x, scale), emu(t.Y, scale), maxInt(emu(w, scale), 1), maxInt(emu(t.H, scale), 1))
	fmt.Fprintf(&b, `<p:txBody><a:bodyPr wrap="%s" lIns="0" tIns="0" rIns="0" bIns="0" rtlCol="0" anchor="t"><a:noAutofit/></a:bodyPr><a:lstStyle/>`, wrap)
	align := t.alignment()
	b.WriteString(`<a:p><a:pPr marL="0" indent="0" algn="` + align + `">`)
	if t.LineH > 0 {
		fmt.Fprintf(&b, `<a:lnSpc><a:spcPts val="%d"/></a:lnSpc>`, int(math.Round(t.LineH*scale/12700*100)))
	}
	b.WriteString(`<a:spcBef><a:spcPts val="0"/></a:spcBef><a:spcAft><a:spcPts val="0"/></a:spcAft><a:buNone/></a:pPr>`)
	var last pptxRun
	for _, r := range t.Runs {
		if !r.Br && !pptxHexColor.MatchString(r.Color) {
			// A colour the format cannot hold is not written into it; the run is left out.
			continue
		}
		if r.Br {
			if last.Size == 0 {
				continue
			}
			b.WriteString(`<a:br>` + last.props("rPr", scale, lang) + `</a:br>`)
			continue
		}
		last = r
		b.WriteString(`<a:r>` + r.props("rPr", scale, lang) + `<a:t>` + pptxEsc(r.Text) + `</a:t></a:r>`)
	}
	if last.Size > 0 {
		b.WriteString(last.props("endParaRPr", scale, lang))
	}
	b.WriteString(`</a:p></p:txBody></p:sp>`)
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const pptxGroupHead = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func pptxRoot(tag string) string {
	return pptxXMLHeader + `<p:` + tag + ` xmlns:a="` + pptxNSa + `" xmlns:r="` + pptxNSr + `" xmlns:p="` + pptxNSp + `"`
}

func (d pptxDeck) slideXML(s pptxSlide, scale float64, cy int) string {
	var b strings.Builder
	b.WriteString(pptxRoot("sld") + `><p:cSld name="` + pptxEsc(s.Title) + `"><p:spTree>` + pptxGroupHead)
	fmt.Fprintf(&b, `<p:pic><p:nvPicPr><p:cNvPr id="2" name="Slide picture" descr="%s"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>`+
		`<p:blipFill><a:blip r:embed="rId2"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>`+
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`,
		pptxEsc(s.Title), pptxWidthEMU, cy)
	texts := s.Texts
	if len(texts) > pptxMaxBoxes {
		texts = texts[:pptxMaxBoxes]
	}
	for i, t := range texts {
		b.WriteString(t.xml(i+3, scale, d.lang()))
	}
	b.WriteString(`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	return b.String()
}

func (d pptxDeck) lang() string {
	if d.Lang != "" {
		return d.Lang
	}
	return "en-US"
}

func notesXML(notes, lang string) string {
	var b strings.Builder
	b.WriteString(pptxRoot("notes") + `><p:cSld><p:spTree>` + pptxGroupHead)
	b.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>`)
	b.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/>`)
	for _, ln := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			b.WriteString(`<a:p><a:endParaRPr lang="` + pptxEsc(lang) + `"/></a:p>`)
			continue
		}
		b.WriteString(`<a:p><a:r><a:rPr lang="` + pptxEsc(lang) + `" dirty="0"/><a:t>` + pptxEsc(ln) + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`)
	return b.String()
}

const clrMap = `<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>`

func textStyleLevel1() string {
	return `<a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1"><a:defRPr sz="1800" kern="1200">` +
		`<a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/><a:cs typeface="+mn-cs"/></a:defRPr></a:lvl1pPr>`
}

func masterXML() string {
	return pptxRoot("sldMaster") + `><p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg><p:spTree>` + pptxGroupHead + `</p:spTree></p:cSld>` +
		clrMap + `<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
		`<p:txStyles><p:titleStyle>` + textStyleLevel1() + `</p:titleStyle><p:bodyStyle>` + textStyleLevel1() + `</p:bodyStyle><p:otherStyle>` +
		textStyleLevel1() + `</p:otherStyle></p:txStyles></p:sldMaster>`
}

func layoutXML() string {
	return pptxRoot("sldLayout") + ` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>` + pptxGroupHead + `</p:spTree></p:cSld>` +
		`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
}

func notesMasterXML() string {
	return pptxRoot("notesMaster") + `><p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg><p:spTree>` + pptxGroupHead +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="sldImg" idx="2"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="685800" y="1143000"/><a:ext cx="5486400" cy="3086100"/></a:xfrm>` +
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln w="12700"><a:solidFill><a:prstClr val="black"/></a:solidFill></a:ln></p:spPr></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" sz="quarter" idx="3"/></p:nvPr></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="685800" y="4400550"/><a:ext cx="5486400" cy="3600450"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr vert="horz" lIns="91440" tIns="45720" rIns="91440" bIns="45720" rtlCol="0"/><a:lstStyle/><a:p><a:pPr lvl="0"/><a:r><a:rPr lang="en-US"/><a:t>Notes</a:t></a:r></a:p></p:txBody></p:sp>` +
		`</p:spTree></p:cSld>` + clrMap + `<p:notesStyle>` + textStyleLevel1() + `</p:notesStyle></p:notesMaster>`
}

func themeXML(name, major, minor string) string {
	fill := `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	grad := `<a:gradFill rotWithShape="1"><a:gsLst><a:gs pos="0"><a:schemeClr val="phClr"><a:tint val="50000"/></a:schemeClr></a:gs>` +
		`<a:gs pos="100000"><a:schemeClr val="phClr"><a:shade val="90000"/></a:schemeClr></a:gs></a:gsLst><a:lin ang="5400000" scaled="0"/></a:gradFill>`
	ln := func(w int) string {
		return fmt.Sprintf(`<a:ln w="%d" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/><a:miter lim="800000"/></a:ln>`, w)
	}
	eff := `<a:effectStyle><a:effectLst/></a:effectStyle>`
	clr := func(tag, val string) string { return `<a:` + tag + `><a:srgbClr val="` + val + `"/></a:` + tag + `>` }
	return pptxXMLHeader + `<a:theme xmlns:a="` + pptxNSa + `" name="` + name + `"><a:themeElements><a:clrScheme name="` + name + `">` +
		`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
		clr("dk2", "44546A") + clr("lt2", "E7E6E6") + clr("accent1", "4472C4") + clr("accent2", "ED7D31") + clr("accent3", "A5A5A5") +
		clr("accent4", "FFC000") + clr("accent5", "5B9BD5") + clr("accent6", "70AD47") + clr("hlink", "0563C1") + clr("folHlink", "954F72") +
		`</a:clrScheme><a:fontScheme name="` + name + `"><a:majorFont><a:latin typeface="` + major + `"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
		`<a:minorFont><a:latin typeface="` + minor + `"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="` + name + `"><a:fillStyleLst>` + fill + fill + grad + `</a:fillStyleLst><a:lnStyleLst>` + ln(6350) + ln(12700) + ln(19050) +
		`</a:lnStyleLst><a:effectStyleLst>` + eff + eff + eff + `</a:effectStyleLst><a:bgFillStyleLst>` + fill + fill + grad + `</a:bgFillStyleLst></a:fmtScheme>` +
		`</a:themeElements></a:theme>`
}

// writePPTX writes the presentation as a zip.
func writePPTX(out io.Writer, d pptxDeck) error {
	if d.W <= 0 || d.H <= 0 || len(d.Slides) == 0 {
		return fmt.Errorf("a presentation needs a size and a slide")
	}
	scale := float64(pptxWidthEMU) / float64(d.W)
	cy := d.heightEMU()
	zw := zip.NewWriter(out)
	// Every part is deflated: LibreOffice cannot read a stored part whose size is only in a trailing descriptor.
	put := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	str := func(name, data string) error { return put(name, []byte(data)) }

	var ct strings.Builder
	ct.WriteString(pptxXMLHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="` + ctRels + `"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>`)
	over := func(part, typ string) {
		ct.WriteString(`<Override PartName="` + part + `" ContentType="` + typ + `"/>`)
	}
	over("/ppt/presentation.xml", ctMain+"presentation.main+xml")
	over("/ppt/slideMasters/slideMaster1.xml", ctMain+"slideMaster+xml")
	over("/ppt/slideLayouts/slideLayout1.xml", ctMain+"slideLayout+xml")
	over("/ppt/notesMasters/notesMaster1.xml", ctMain+"notesMaster+xml")
	over("/ppt/theme/theme1.xml", ctTheme)
	over("/ppt/theme/theme2.xml", ctTheme)
	over("/ppt/presProps.xml", ctMain+"presProps+xml")
	over("/ppt/viewProps.xml", ctMain+"viewProps+xml")
	over("/ppt/tableStyles.xml", ctMain+"tableStyles+xml")
	over("/docProps/core.xml", ctCore)
	over("/docProps/app.xml", ctApp)
	for i, s := range d.Slides {
		over(fmt.Sprintf("/ppt/slides/slide%d.xml", i+1), ctMain+"slide+xml")
		if strings.TrimSpace(s.Notes) != "" {
			over(fmt.Sprintf("/ppt/notesSlides/notesSlide%d.xml", i+1), ctMain+"notesSlide+xml")
		}
	}
	ct.WriteString(`</Types>`)
	if err := str("[Content_Types].xml", ct.String()); err != nil {
		return err
	}
	if err := str("_rels/.rels", relsXML([][3]string{
		{"rId1", pptxRelBase + "officeDocument", "ppt/presentation.xml"},
		{"rId2", "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", "docProps/core.xml"},
		{"rId3", pptxRelBase + "extended-properties", "docProps/app.xml"},
	})); err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := str("docProps/core.xml", pptxXMLHeader+`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" `+
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		`<dc:title>`+pptxEsc(d.Title)+`</dc:title><dcterms:created xsi:type="dcterms:W3CDTF">`+now+`</dcterms:created>`+
		`<dcterms:modified xsi:type="dcterms:W3CDTF">`+now+`</dcterms:modified></cp:coreProperties>`); err != nil {
		return err
	}
	if err := str("docProps/app.xml", pptxXMLHeader+`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">`+
		`<Application>harnessly</Application><Slides>`+strconv.Itoa(len(d.Slides))+`</Slides></Properties>`); err != nil {
		return err
	}

	presRels := [][3]string{
		{"rId1", pptxRelBase + "slideMaster", "slideMasters/slideMaster1.xml"},
		{"rId2", pptxRelBase + "notesMaster", "notesMasters/notesMaster1.xml"},
		{"rId3", pptxRelBase + "theme", "theme/theme1.xml"},
		{"rId4", pptxRelBase + "presProps", "presProps.xml"},
		{"rId5", pptxRelBase + "viewProps", "viewProps.xml"},
		{"rId6", pptxRelBase + "tableStyles", "tableStyles.xml"},
	}
	var ids strings.Builder
	for i := range d.Slides {
		rid := fmt.Sprintf("rId%d", 10+i)
		presRels = append(presRels, [3]string{rid, pptxRelBase + "slide", fmt.Sprintf("slides/slide%d.xml", i+1)})
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="%s"/>`, 256+i, rid)
	}
	if err := str("ppt/presentation.xml", pptxRoot("presentation")+` saveSubsetFonts="1"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>`+
		`<p:notesMasterIdLst><p:notesMasterId r:id="rId2"/></p:notesMasterIdLst><p:sldIdLst>`+ids.String()+`</p:sldIdLst>`+
		fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/>`, pptxWidthEMU, cy)+`<p:notesSz cx="6858000" cy="9144000"/></p:presentation>`); err != nil {
		return err
	}
	if err := str("ppt/_rels/presentation.xml.rels", relsXML(presRels)); err != nil {
		return err
	}
	if err := str("ppt/presProps.xml", pptxRoot("presentationPr")+`/>`); err != nil {
		return err
	}
	if err := str("ppt/viewProps.xml", pptxRoot("viewPr")+`/>`); err != nil {
		return err
	}
	if err := str("ppt/tableStyles.xml", pptxXMLHeader+`<a:tblStyleLst xmlns:a="`+pptxNSa+`" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`); err != nil {
		return err
	}
	if err := str("ppt/theme/theme1.xml", themeXML("Deck", "Arial", "Arial")); err != nil {
		return err
	}
	if err := str("ppt/theme/theme2.xml", themeXML("Notes", "Arial", "Arial")); err != nil {
		return err
	}
	if err := str("ppt/slideMasters/slideMaster1.xml", masterXML()); err != nil {
		return err
	}
	if err := str("ppt/slideMasters/_rels/slideMaster1.xml.rels", relsXML([][3]string{
		{"rId1", pptxRelBase + "slideLayout", "../slideLayouts/slideLayout1.xml"},
		{"rId2", pptxRelBase + "theme", "../theme/theme1.xml"},
	})); err != nil {
		return err
	}
	if err := str("ppt/slideLayouts/slideLayout1.xml", layoutXML()); err != nil {
		return err
	}
	if err := str("ppt/slideLayouts/_rels/slideLayout1.xml.rels", relsXML([][3]string{
		{"rId1", pptxRelBase + "slideMaster", "../slideMasters/slideMaster1.xml"},
	})); err != nil {
		return err
	}
	if err := str("ppt/notesMasters/notesMaster1.xml", notesMasterXML()); err != nil {
		return err
	}
	if err := str("ppt/notesMasters/_rels/notesMaster1.xml.rels", relsXML([][3]string{
		{"rId1", pptxRelBase + "theme", "../theme/theme2.xml"},
	})); err != nil {
		return err
	}
	for i, s := range d.Slides {
		n := i + 1
		rels := [][3]string{
			{"rId1", pptxRelBase + "slideLayout", "../slideLayouts/slideLayout1.xml"},
			{"rId2", pptxRelBase + "image", fmt.Sprintf("../media/image%d.png", n)},
		}
		if strings.TrimSpace(s.Notes) != "" {
			rels = append(rels, [3]string{"rId3", pptxRelBase + "notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", n)})
			if err := str(fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n), notesXML(s.Notes, d.lang())); err != nil {
				return err
			}
			if err := str(fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", n), relsXML([][3]string{
				{"rId1", pptxRelBase + "notesMaster", "../notesMasters/notesMaster1.xml"},
				{"rId2", pptxRelBase + "slide", fmt.Sprintf("../slides/slide%d.xml", n)},
			})); err != nil {
				return err
			}
		}
		if err := str(fmt.Sprintf("ppt/slides/slide%d.xml", n), d.slideXML(s, scale, cy)); err != nil {
			return err
		}
		if err := str(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), relsXML(rels)); err != nil {
			return err
		}
		if err := put(fmt.Sprintf("ppt/media/image%d.png", n), s.Picture); err != nil {
			return err
		}
	}
	return zw.Close()
}
