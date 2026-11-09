// A node of the kit's canvas by its id (the tree, the canvas, the search and a link name it the same way): what it is,
// where it lies, how a board calls it, what goes to a chat about it. Pure.
import type {
  IColorToken,
  IKitAsset,
  IKitAssetGroup,
  IKitCanvas,
  IKitComponent,
  IKitComponentGroup,
  IKitLayout,
  IKitTemplate,
  IKitTemplateSection,
  IPlainToken,
  ITypeStyle,
  TPropValue,
} from '@/next/features/designly/kit/kit-api';

export const NODE = {
  component: 'component',
  layout: 'layout',
  template: 'template',
  tsection: 'tsection',
  asset: 'asset',
  color: 'color',
  type: 'type',
  token: 'token',
} as const;

export type TNode =
  | { kind: typeof NODE.component; component: IKitComponent; group: IKitComponentGroup }
  | { kind: typeof NODE.layout; layout: IKitLayout; template: IKitTemplate; section: IKitTemplateSection }
  | { kind: typeof NODE.template; template: IKitTemplate }
  | { kind: typeof NODE.tsection; template: IKitTemplate; section: IKitTemplateSection }
  | { kind: typeof NODE.asset; asset: IKitAsset; group: IKitAssetGroup }
  | { kind: typeof NODE.color; color: IColorToken }
  | { kind: typeof NODE.type; style: ITypeStyle }
  | { kind: typeof NODE.token; token: IPlainToken };

export const findNode = (c: IKitCanvas, node: string): TNode | null => {
  for (const group of c.groups ?? [])
    for (const component of group.components)
      if (component.node === node) return { kind: NODE.component, component, group };
  for (const template of c.templates ?? []) {
    if (template.node === node) return { kind: NODE.template, template };
    for (const section of template.sections) {
      if (section.node === node) return { kind: NODE.tsection, template, section };
      for (const layout of section.layouts)
        if (layout.node === node) return { kind: NODE.layout, layout, template, section };
    }
  }
  for (const group of c.assets ?? [])
    for (const asset of group.items) if (asset.node === node) return { kind: NODE.asset, asset, group };
  for (const color of c.colors ?? []) if (color.id === node) return { kind: NODE.color, color };
  for (const style of c.type ?? []) if (style.id === node) return { kind: NODE.type, style };
  for (const token of c.tokens ?? []) if (token.id === node) return { kind: NODE.token, token };
  return null;
};

const attrValue = (v: TPropValue): string | null => {
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  return null;
};

const escapeAttr = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');

/** How a board calls a component with props: plain values as attributes, objects as a JSON attribute. */
export const xImport = (ns: string, component: string, props: Record<string, TPropValue>): string => {
  const attrs = Object.keys(props)
    .sort()
    .map((k) => {
      const v = props[k] as TPropValue;
      const plain = attrValue(v);
      if (plain !== null) return ` ${k}="${escapeAttr(plain)}"`;
      if (v === null) return '';
      return ` ${k}='${JSON.stringify(v).replace(/'/g, '&#39;')}'`;
    })
    .join('');
  return `<x-import component-from-global-scope="${ns}.${component}"${attrs}></x-import>`;
};

/** The call of a node with its own props and the person's changes (`extra`); null for what is not called. */
export const callOf = (c: IKitCanvas, n: TNode, extra: Record<string, TPropValue> = {}): string | null => {
  if (n.kind === NODE.component)
    return xImport(c.namespace, n.component.id, { ...(n.component.sample ?? {}), ...extra });
  if (n.kind === NODE.layout && n.template.component)
    return xImport(c.namespace, n.template.component, { ...(n.layout.props ?? {}), ...extra });
  if (n.kind === NODE.asset && n.group.component && n.group.prop)
    return xImport(c.namespace, n.group.component, { [n.group.prop]: n.asset.name, ...extra });
  if (n.kind === NODE.color) return Object.values(n.color.values)[0] ?? null;
  if (n.kind === NODE.token) return n.token.value;
  return null;
};

export const nameOf = (n: TNode): string => {
  switch (n.kind) {
    case NODE.component:
      return n.component.name;
    case NODE.layout:
      return n.layout.name;
    case NODE.template:
      return n.template.title;
    case NODE.tsection:
      return n.section.title;
    case NODE.asset:
      return n.asset.name;
    case NODE.color:
      return n.color.name;
    case NODE.type:
      return `${n.style.group} · ${n.style.name}`;
    case NODE.token:
      return n.token.name;
  }
};

/** Where a node lies, as the breadcrumbs say it: the section's name and the groups under it. */
export const placeOf = (n: TNode, titles: Record<string, string>): string[] => {
  switch (n.kind) {
    case NODE.component:
      return [titles.components ?? '', n.group.title];
    case NODE.layout:
      return [titles[n.template.section || 'templates'] ?? '', n.template.title, n.section.title];
    case NODE.template:
      return [titles[n.template.section || 'templates'] ?? ''];
    case NODE.tsection:
      return [titles[n.template.section || 'templates'] ?? '', n.template.title];
    case NODE.asset:
      return [titles.assets ?? '', n.group.title];
    case NODE.color:
      return [titles.foundation ?? '', titles.colors ?? ''];
    case NODE.type:
      return [titles.foundation ?? '', titles.type ?? ''];
    case NODE.token:
      return [titles.foundation ?? '', titles.tokens ?? ''];
  }
};

/** The text «Отправить в чат» puts into the field: the node, where it is, how to call it, the link to it. */
export const chatTextOf = (
  c: IKitCanvas,
  n: TNode,
  words: { kit: string; call: string; slots: string; link: string },
  link: string,
  extra: Record<string, TPropValue> = {},
): string => {
  const lines = [`${words.kit} «${c.title}» ${c.version} · ${c.theme}: ${nameOf(n)}`];
  const call = callOf(c, n, extra);
  if (call) lines.push(`${words.call}: ${call}`);
  if (n.kind === NODE.layout) {
    if (n.layout.address) lines.push(`layout: ${n.layout.address}`);
    const slots = n.layout.slots.map((s) =>
      s.sample ? `${s.key} (${s.kind}): ${s.sample}` : `${s.key} (${s.kind})`,
    );
    if (slots.length > 0) lines.push(`${words.slots}: ${slots.join('; ')}`);
  }
  lines.push(`${words.link}: ${link}`);
  return lines.join('\n');
};
