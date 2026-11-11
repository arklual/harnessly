// What drawing a board takes, for the pages turned from boards by `migrate --to tsx`: the values its template reads,
// its logic class, the components it takes from window. Written once into pages/board-engine/index.ts of a project;
// its pages import it. It does what the board format (texts/format.md) says a board does, so a page draws the DOM
// its board drew.
import React from "react";

type AnyProps = Record<string, any>;

// DCLogic is the base of a board's logic class: props, state, setState, the lifecycle, renderVals().
export class DCLogic {
  props: AnyProps;
  state: AnyProps;
  __redraw?: () => void;
  __after?: Array<() => void>;
  constructor(props?: AnyProps) {
    this.props = props || {};
    this.state = this.state || {};
  }
  setState(update: any, done?: () => void) {
    const next = typeof update === "function" ? update(this.state, this.props) : update;
    if (next) this.state = { ...this.state, ...next };
    if (!this.__redraw) {
      if (done) done.call(this);
      return;
    }
    if (done) (this.__after = this.__after || []).push(done);
    this.__redraw();
  }
  forceUpdate() {
    if (this.__redraw) this.__redraw();
  }
  componentDidMount() {}
  componentDidUpdate(_previous: AnyProps) {}
  componentWillUnmount() {}
  renderVals(): AnyProps {
    return {};
  }
}

function attempt(logic: any, method: string, args: unknown[] = []) {
  try {
    logic[method](...args);
  } catch (error) {
    console.error(method + "():", error);
  }
}

// useBoard runs a board's logic for a page and gives the values its template reads: the props, then renderVals().
export function useBoard(Logic: any, props: AnyProps): AnyProps {
  const [, redraw] = React.useReducer((n: number) => n + 1, 0);
  const ref = React.useRef<any>(null);
  if (!ref.current) {
    let logic: any;
    try {
      logic = new (Logic || DCLogic)(props);
    } catch (error) {
      console.error("constructor failed:", error);
      logic = new DCLogic(props);
    }
    if (!logic.state) logic.state = {};
    ref.current = logic;
  }
  const logic = ref.current;
  logic.props = props;
  const mounted = React.useRef(false);
  const drawn = React.useRef(props);
  React.useLayoutEffect(() => {
    logic.__redraw = redraw;
    attempt(logic, "componentDidMount");
    return () => {
      logic.__redraw = undefined;
      attempt(logic, "componentWillUnmount");
    };
  }, [logic]);
  React.useLayoutEffect(() => {
    if (mounted.current) attempt(logic, "componentDidUpdate", [drawn.current]);
    mounted.current = true;
    drawn.current = props;
    const done = (logic.__after || []).splice(0);
    done.forEach((fn: () => void) => fn.call(logic));
  });
  let values: AnyProps | null = null;
  try {
    values = logic.renderVals();
  } catch (error) {
    console.error("renderVals():", error);
  }
  return { ...props, ...(values || {}) };
}

// show is a {{ value }} in text: nothing for null, undefined and booleans, a list or an element as it is, anything
// else in a <span class="sc-interp">.
export function show(value: unknown): React.ReactNode {
  if (value === undefined || value === null || typeof value === "boolean") return null;
  if (Array.isArray(value) || React.isValidElement(value)) return value as React.ReactNode;
  return React.createElement("span", { className: "sc-interp" }, String(value));
}

// list is what sc-for walks: the value when it is an array, else nothing.
export function list(value: unknown): any[] {
  return Array.isArray(value) ? value : [];
}

// fn is an event's value: a function, else nothing.
export function fn(value: unknown): any {
  return typeof value === "function" ? value : undefined;
}

// ref is a ref's value: a function or an object, else nothing.
export function ref(value: unknown): any {
  return typeof value === "function" || (typeof value === "object" && value !== null) ? value : undefined;
}

// css is a style given as text, as an object (custom properties as they are, !important dropped); an object stays
// as it is, and nothing gives no style.
export function css(value: unknown): any {
  if (value === undefined || value === null || value === false) return undefined;
  if (typeof value === "object") return value;
  const text = String(value);
  const out: AnyProps = {};
  let start = 0;
  let depth = 0;
  let quote = "";
  const take = (decl: string) => {
    const colon = decl.indexOf(":");
    if (colon <= 0) return;
    let name = decl.slice(0, colon).trim();
    const v = decl.slice(colon + 1).replace(/!\s*important\s*$/i, "").trim();
    if (!name || !v) return;
    if (!name.startsWith("--")) {
      name = name.toLowerCase().replace(/^-ms-/, "ms-").replace(/-([a-z])/g, (_, c) => c.toUpperCase());
    }
    out[name] = v;
  };
  for (let i = 0; i < text.length; i += 1) {
    const c = text.charAt(i);
    if (quote) {
      if (c === quote) quote = "";
      continue;
    }
    if (c === '"' || c === "'") quote = c;
    else if (c === "(") depth += 1;
    else if (c === ")") depth = Math.max(0, depth - 1);
    else if (c === ";" && depth === 0) {
      take(text.slice(start, i));
      start = i + 1;
    }
  }
  take(text.slice(start));
  return out;
}

const HOST_STYLE = new Set(["width", "height", "position", "top", "left", "right", "bottom", "inset", "zIndex", "transform"]);

// hostStyle is the box of an <x-import>: only its place and size, else display: contents.
export function hostStyle(value: unknown): any {
  const style = css(value);
  const kept: AnyProps = {};
  if (style) for (const k of Object.keys(style)) if (HOST_STYLE.has(k)) kept[k] = style[k];
  return Object.keys(kept).length ? kept : { display: "contents" };
}

function globalAt(path: string): any {
  let value: any = window;
  for (const key of path.split(".")) {
    if (value === undefined || value === null) return undefined;
    value = value[key];
  }
  return typeof value === "function" || (value && typeof value === "object" && value.$$typeof) ? value : undefined;
}

// Global is <x-import component-from-global-scope>: the component at that path on window, drawn with the props; a
// placeholder until a script puts it there.
export function Global({ __global, __hint, ...props }: AnyProps) {
  const component = typeof __global === "string" ? globalAt(__global) : undefined;
  const [, redraw] = React.useReducer((n: number) => n + 1, 0);
  React.useEffect(() => {
    if (component || typeof __global !== "string" || !__global) return;
    let waited = 0;
    let timer: ReturnType<typeof setTimeout>;
    const look = () => {
      waited += 1;
      if (globalAt(__global)) redraw();
      else timer = setTimeout(look, waited < 150 ? 100 : 2000);
    };
    timer = setTimeout(look, 100);
    return () => clearTimeout(timer);
  }, [component, __global]);
  if (component) return React.createElement(component, props);
  const style: AnyProps = { width: "100%", height: "60px" };
  if (typeof __hint === "string" && __hint) {
    const [w, h] = __hint.split(",");
    delete style.width;
    delete style.height;
    if (w && w.trim()) style.width = w.trim();
    if (h && h.trim()) style.height = h.trim();
  }
  return React.createElement("div", { className: "sc-placeholder", title: __global == null ? "" : String(__global), style });
}
