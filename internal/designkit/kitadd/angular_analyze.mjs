// Reads the Angular components an npm package declares: node angular_analyze.mjs <package-dir-with-node_modules> <package>.
// A component is a class with `static ɵcmp: ɵɵComponentDeclaration<Class, selector, exportAs, inputs, outputs, queries,
// ngContentSelectors, standalone, ...>` in the package's .d.ts, whichever entry point (secondary entry) exports it.
// Prints JSON: { entries: [subpath...], components: [{ name, entry, selector, standalone, description, inputs, outputs, slots }] }.
import { createRequire } from 'node:module';
import path from 'node:path';
import fs from 'node:fs';

const dir = path.resolve(process.argv[2]);
const pkg = process.argv[3];
const require = createRequire(path.join(dir, 'noop.js'));
const ts = require('typescript');
const pj = JSON.parse(fs.readFileSync(path.join(dir, 'node_modules', pkg, 'package.json'), 'utf8'));

const entries = [];
const typesOf = (v) => (typeof v === 'string' ? (v.endsWith('.d.ts') ? v : null) : v && typeof v === 'object' ? (v.types || (v.import && typesOf(v.import)) || typesOf(v.default)) : null);
for (const [k, v] of Object.entries(pj.exports || {})) {
  if (k.includes('*') || /\.(css|scss|json)$/.test(k) || k === './package.json' || /(^|\/)(testing|schematics|migrations)(\/|$)/.test(k)) continue;
  let t = typesOf(v);
  if (!t && typeof v === 'object') t = typesOf(v);
  if (typeof t !== 'string') continue;
  const file = path.resolve(dir, 'node_modules', pkg, t);
  if (fs.existsSync(file)) entries.push({ sub: k === '.' ? '' : k.slice(2), file });
}
if (!entries.length && pj.types) entries.push({ sub: '', file: path.resolve(dir, 'node_modules', pkg, pj.types) });

const program = ts.createProgram(entries.map((e) => e.file), {
  module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022, moduleResolution: ts.ModuleResolutionKind.Bundler,
  skipLibCheck: true, noEmit: true, strict: false,
});
const checker = program.getTypeChecker();
const doc = (sym) => ts.displayPartsToString(sym.getDocumentationComment(checker)).trim().split(/\n\s*\n/)[0].replace(/\s+/g, ' ');
const lit = (n) => (n && (ts.isLiteralTypeNode(n) && ts.isStringLiteral(n.literal)) ? n.literal.text : null);
const strip = (t) => (t.isUnion() ? t.types.filter((x) => !(x.flags & (ts.TypeFlags.Undefined | ts.TypeFlags.Null))) : [t]);

function kind(t) {
  const parts = strip(t);
  if (!parts.length) return { type: 'string' };
  if (parts.every((p) => p.flags & ts.TypeFlags.BooleanLike)) return { type: 'boolean' };
  if (parts.every((p) => p.isStringLiteral())) return { type: 'enum', values: parts.map((p) => p.value) };
  if (parts.every((p) => p.flags & ts.TypeFlags.NumberLike)) return { type: 'number' };
  if (parts.every((p) => p.flags & ts.TypeFlags.StringLike)) return { type: 'string' };
  if (parts.some((p) => p.getCallSignatures().length)) return { type: 'function' };
  if (parts.some((p) => checker.isArrayType(p) || checker.isTupleType(p))) return { type: 'array' };
  return { type: 'object' };
}
// InputSignal<T>, InputSignalWithTransform<T, U>, ModelSignal<T>, OutputEmitterRef<T>, EventEmitter<T> → T
function inner(t) {
  const n = t.aliasSymbol ? t.aliasSymbol.getName() : t.symbol ? t.symbol.getName() : '';
  if (/^(InputSignal|InputSignalWithTransform|ModelSignal|OutputEmitterRef|EventEmitter|OutputRef)$/.test(n)) {
    const a = t.aliasTypeArguments || checker.getTypeArguments(t);
    if (a && a[0]) return a[0];
  }
  return t;
}
function members(typeNode) {
  const out = {};
  if (typeNode && ts.isTypeLiteralNode(typeNode)) {
    for (const m of typeNode.members) {
      if (!ts.isPropertySignature(m) || !m.name) continue;
      const k = m.name.getText().replace(/^["']|["']$/g, '');
      out[k] = m.type;
    }
  }
  return out;
}
const seen = new Set();
const comps = [];
for (const e of entries) {
  const sf = program.getSourceFile(e.file);
  const ms = sf && checker.getSymbolAtLocation(sf);
  if (!ms) continue;
  for (const ex of checker.getExportsOfModule(ms)) {
    let sym = ex;
    if (sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
    const decl = (sym.getDeclarations() || []).find((d) => ts.isClassDeclaration(d));
    if (!decl) continue;
    const name = ex.getName();
    if (seen.has(name)) continue;
    let cmp = decl.members.find((m) => ts.isPropertyDeclaration(m) && m.name && m.name.getText() === 'ɵcmp');
    let isDir = false;
    if (!cmp) { cmp = decl.members.find((m) => ts.isPropertyDeclaration(m) && m.name && m.name.getText() === 'ɵdir'); isDir = true; }
    if (!cmp || !cmp.type || !ts.isTypeReferenceNode(cmp.type)) continue;
    const a = cmp.type.typeArguments || [];
    if (a.length < 8) continue;
    seen.add(name);
    const standalone = a[7] && a[7].getText() === 'true';
    const inputsN = members(a[3]);
    const outputsN = members(a[4]);
    // Inputs and outputs of the base classes (a panel's `expanded` is the accordion item's): the d.ts of the class lists only its own.
    let cur = decl, hops = 0;
    while (cur && hops++ < 8) {
      const hc = (cur.heritageClauses || []).find((h) => h.token === ts.SyntaxKind.ExtendsKeyword);
      const ex0 = hc && hc.types[0] && hc.types[0].expression;
      let bs = ex0 && checker.getSymbolAtLocation(ex0);
      if (bs && bs.flags & ts.SymbolFlags.Alias) bs = checker.getAliasedSymbol(bs);
      const bd = bs && (bs.getDeclarations() || []).find((d) => ts.isClassDeclaration(d));
      if (!bd) break;
      const bm = bd.members.find((m) => ts.isPropertyDeclaration(m) && m.name && ['ɵdir', 'ɵcmp'].includes(m.name.getText()));
      if (bm && bm.type && ts.isTypeReferenceNode(bm.type) && (bm.type.typeArguments || []).length >= 5) {
        const ba = bm.type.typeArguments;
        for (const [k, v] of Object.entries(members(ba[3]))) if (!(k in inputsN)) inputsN[k] = v;
        for (const [k, v] of Object.entries(members(ba[4]))) if (!(k in outputsN)) outputsN[k] = v;
      }
      cur = bd;
    }
    // hostDirectives: the inputs and outputs a host directive exposes under an alias belong to the component too.
    const hostIn = [], hostOut = [];
    if (a[8] && ts.isTupleTypeNode(a[8])) {
      for (const el of a[8].elements) {
        if (!ts.isTypeLiteralNode(el)) continue;
        const hm = members(el);
        for (const [k, v] of Object.entries(members(hm.inputs))) { const al = lit(v); if (al) hostIn.push({ from: k, alias: al }); }
        for (const [k, v] of Object.entries(members(hm.outputs))) { const al = lit(v); if (al) hostOut.push({ from: k, alias: al }); }
      }
    }
    const inst = checker.getDeclaredTypeOfSymbol(sym);
    const props = [];
    for (const h of hostIn) if (!props.some((p) => p.name === h.alias) && !Object.keys(inputsN).includes(h.alias)) props.push({ name: h.alias, prop: h.alias, type: 'string', required: false, description: '' });
    for (const [pname, tn] of Object.entries(inputsN)) {
      const am = members(tn);
      const alias = lit(am.alias) || pname;
      const required = am.required && am.required.getText() === 'true';
      const ms2 = checker.getPropertyOfType(inst, pname);
      let t = null, description = '';
      if (ms2) { t = inner(checker.getTypeOfSymbolAtLocation(ms2, decl)); description = doc(ms2); }
      const k = t ? kind(t) : { type: 'string' };
      props.push({ name: alias, prop: pname, ...k, required: !!required, description });
    }
    const outs = [];
    for (const [pname, tn] of Object.entries(outputsN)) {
      const alias = lit(tn) || pname;
      const ms2 = checker.getPropertyOfType(inst, pname);
      outs.push({ name: alias, prop: pname, description: ms2 ? doc(ms2) : '' });
    }
    for (const h of hostOut) if (!outs.some((p) => p.name === h.alias)) outs.push({ name: h.alias, prop: h.alias, description: '' });
    let slots = [];
    if (a[6] && ts.isTupleTypeNode(a[6])) slots = a[6].elements.map((x) => lit(x) ?? '*').filter(Boolean);
    comps.push({
      name, kind: isDir ? 'directive' : 'component', cva: !!(checker.getPropertyOfType(inst, 'writeValue') && checker.getPropertyOfType(inst, 'registerOnChange')), entry: e.sub, selector: lit(a[1]) || '', standalone, description: doc(sym), inputs: props, outputs: outs, slots,
    });
  }
}
process.stdout.write(JSON.stringify({ entries: entries.map((e) => e.sub), components: comps }));
