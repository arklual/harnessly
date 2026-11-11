// Reads the exports of a module with the TypeScript compiler and prints, as JSON, which of them are React components
// and what props they take. Run by `harnessly design kit add` with node, from the build folder that has typescript in
// node_modules: node analyze.mjs <entry.ts>. Props declared by React's own DOM types (className, onClick, aria-*) are
// left out: a kit lists what the library adds. children is kept when the component takes it.
import { createRequire } from 'node:module';
import path from 'node:path';

const entry = path.resolve(process.argv[2]);
const require = createRequire(path.join(path.dirname(entry), 'noop.js'));
const ts = require('typescript');

const program = ts.createProgram([entry], {
  jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020,
  moduleResolution: ts.ModuleResolutionKind.Bundler, allowJs: false, skipLibCheck: true, strict: true,
  esModuleInterop: true, noEmit: true,
});
const checker = program.getTypeChecker();
const sf = program.getSourceFile(entry);
const out = { exports: {} };
const modSym = sf && checker.getSymbolAtLocation(sf);
if (!modSym) { process.stdout.write(JSON.stringify(out)); process.exit(0); }

const reactOwn = (decl) => {
  const f = decl.getSourceFile().fileName.replace(/\\/g, '/');
  return /\/node_modules\/(@types\/react|csstype)\//.test(f) || /\/typescript\/lib\//.test(f);
};
const doc = (sym) => ts.displayPartsToString(sym.getDocumentationComment(checker)).trim().split(/\n\s*\n/)[0].replace(/\s+/g, ' ');
const strip = (t) => (t.isUnion() ? t.types.filter((x) => !(x.flags & (ts.TypeFlags.Undefined | ts.TypeFlags.Null | ts.TypeFlags.Void))) : [t]);

function kind(t) {
  const parts = strip(t);
  if (parts.length === 0) return { type: 'string' };
  if (parts.every((p) => p.flags & ts.TypeFlags.BooleanLiteral || p.flags & ts.TypeFlags.Boolean)) return { type: 'boolean' };
  if (parts.every((p) => p.isStringLiteral())) return { type: 'enum', values: parts.map((p) => p.value) };
  if (parts.every((p) => p.isStringLiteral() || p.flags & ts.TypeFlags.BooleanLiteral) && parts.some((p) => p.isStringLiteral())) {
    return { type: 'enum', values: parts.filter((p) => p.isStringLiteral()).map((p) => p.value) };
  }
  const text = checker.typeToString(t, undefined, ts.TypeFormatFlags.NoTruncation);
  if (/ReactNode|ReactElement|JSX\.Element/.test(text)) return { type: 'node' };
  if (parts.every((p) => p.flags & (ts.TypeFlags.Number | ts.TypeFlags.NumberLiteral))) return { type: 'number' };
  if (parts.every((p) => p.flags & (ts.TypeFlags.String | ts.TypeFlags.StringLiteral | ts.TypeFlags.TemplateLiteral))) return { type: 'string' };
  if (parts.some((p) => p.getCallSignatures().length > 0)) return { type: 'function' };
  if (parts.some((p) => checker.isArrayType(p) || checker.isTupleType(p))) return { type: 'array' };
  if (parts.some((p) => p.flags & ts.TypeFlags.Object)) return { type: 'object' };
  if (parts.some((p) => p.flags & (ts.TypeFlags.String | ts.TypeFlags.StringLiteral))) return { type: 'string' };
  if (parts.some((p) => p.flags & (ts.TypeFlags.Number | ts.TypeFlags.NumberLiteral))) return { type: 'number' };
  return { type: 'object' };
}

function propsOf(sig, at) {
  const p = sig.getParameters()[0];
  if (!p) return [];
  const pt = checker.getTypeOfSymbolAtLocation(p, at);
  const list = [];
  // An <input>, <img> or <textarea> takes no children: React throws when a sample gives it some.
  const voidEl = checker.getPropertiesOfType(pt).some((prop) => (prop.getDeclarations() || []).some((d) =>
    d.parent && d.parent.name && /^(Input|Img|Textarea|Hr|Br|Area|Embed|Source|Track|Col|Meta|Link)HTMLAttributes$/.test(d.parent.name.text)));
  for (const prop of checker.getPropertiesOfType(pt)) {
    const decls = prop.getDeclarations() || [];
    const name = prop.getName();
    if (name === 'key' || name === 'ref' || (name === 'children' && voidEl)) continue;
    if (name !== 'children' && (decls.length === 0 || decls.every(reactOwn))) continue;
    const t = checker.getTypeOfSymbolAtLocation(prop, at);
    const k = kind(t);
    if (name === 'children') k.type = k.type === 'function' ? 'function' : 'node';
    const item = { name, ...k, required: !(prop.flags & ts.SymbolFlags.Optional) };
    const d = doc(prop);
    if (d) item.description = d;
    list.push(item);
    if (list.length >= 60) break;
  }
  return list;
}

for (let sym of checker.getExportsOfModule(modSym)) {
  const name = sym.getName();
  if (sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
  const decl = (sym.getDeclarations() || [])[0];
  if (!decl) continue;
  if (!(sym.flags & ts.SymbolFlags.Value)) continue; // a type only
  const t = checker.getTypeOfSymbolAtLocation(sym, decl);
  const sigs = t.getCallSignatures().length ? t.getCallSignatures() : t.getConstructSignatures();
  const rec = { kind: 'other', pos: decl.getStart(), file: path.relative(path.dirname(entry), decl.getSourceFile().fileName).replace(/\\/g, '/') };
  const d = doc(sym);
  if (d) rec.description = d;
  if (/^[A-Z]/.test(name) && sigs.length > 0) {
    const ret = checker.typeToString(sigs[0].getReturnType());
    const ctor = t.getConstructSignatures().length > 0;
    if (ctor || /ReactNode|ReactElement|Element|null/.test(ret) || /ExoticComponent|FC|FunctionComponent/.test(checker.typeToString(t))) {
      rec.kind = 'component';
      rec.props = propsOf(sigs[0], decl);
    }
  }
  out.exports[name] = rec;
}
process.stdout.write(JSON.stringify(out));
