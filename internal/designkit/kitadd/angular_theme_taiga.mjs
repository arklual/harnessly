// Prints the stylesheet of Taiga UI: its LESS theme, fonts and component styles compiled to CSS.
import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
const dir = path.resolve(process.argv[2]);
const req = createRequire(path.join(dir, 'noop.js'));
const less = req('less');
const styles = path.dirname(req.resolve('@taiga-ui/styles/package.json'));
const parts = ['@import "@taiga-ui/styles/taiga-ui-theme.less";', '@import "@taiga-ui/styles/taiga-ui-fonts.less";'];
for (const f of fs.readdirSync(path.join(styles, 'components')).sort()) {
  if (f.endsWith('.less')) parts.push(`@import "@taiga-ui/styles/components/${f}";`);
}
const out = await less.render(parts.join('\n'), { paths: [path.join(dir, 'node_modules')], filename: path.join(dir, 'theme.less') });
process.stdout.write(out.css);
