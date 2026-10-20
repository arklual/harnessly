// Lists every Russian text literal of the page with its position and shape, using the TypeScript parser.
const ts = require(process.argv[2]);
const fs = require('fs');
const path = require('path');
const root = process.argv[3];
const out = [];
const CYR = /[А-Яа-яЁё]/;
const walk = (dir) => {
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name);
    if (fs.statSync(p).isDirectory()) {
      if (name !== 'test') walk(p);
      continue;
    }
    if (!/\.(ts|tsx)$/.test(name)) continue;
    const text = fs.readFileSync(p, 'utf8');
    if (!CYR.test(text)) continue;
    const sf = ts.createSourceFile(
      p,
      text,
      ts.ScriptTarget.Latest,
      true,
      name.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const visit = (node) => {
      const rel = path.relative(root, p);
      const add = (kind, extra) => {
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        out.push({ file: rel, line: line + 1, start: node.getStart(sf), end: node.getEnd(), kind, ...extra });
      };
      if (ts.isStringLiteral(node) && CYR.test(node.text)) {
        const parent = node.parent;
        const inImport = ts.isImportDeclaration(parent) || ts.isExportDeclaration(parent);
        const asKey = ts.isPropertyAssignment(parent) && parent.name === node;
        const inJsxAttr = ts.isJsxAttribute(parent);
        if (!inImport) add('string', { text: node.text, asKey, jsxAttr: inJsxAttr });
      } else if (ts.isNoSubstitutionTemplateLiteral(node) && CYR.test(node.text)) {
        add('string', { text: node.text, jsxAttr: false });
      } else if (ts.isTemplateExpression(node) && CYR.test(node.getText(sf))) {
        const parts = [node.head.text];
        const exprs = [];
        for (const span of node.templateSpans) {
          exprs.push(span.expression.getText(sf));
          parts.push(span.literal.text);
        }
        add('template', { parts, exprs });
        return; // nested templates inside expressions are rare; handled by hand
      } else if (ts.isJsxText(node) && CYR.test(node.text)) {
        add('jsx', { text: node.text });
      } else if (ts.isRegularExpressionLiteral(node) && CYR.test(node.text)) {
        add('regex', { text: node.text });
      }
      ts.forEachChild(node, visit);
    };
    visit(sf);
  }
};
walk(root);
fs.writeFileSync(process.argv[4], JSON.stringify(out, null, 1));
const by = {};
for (const e of out) by[e.kind] = (by[e.kind] || 0) + 1;
console.log(out.length, JSON.stringify(by));
