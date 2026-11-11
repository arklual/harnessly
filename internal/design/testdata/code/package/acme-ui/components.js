// The components of @acme/ui, written once for both the package (index.js) and the kit's browser bundle.
export function make(React) {
  var h = React.createElement;
  function cx() {
    return Array.prototype.filter.call(arguments, Boolean).join(" ");
  }
  function Root(props) {
    return h("div", { className: "acme-root" }, props.children);
  }
  function Stack(props) {
    return h("div", { className: "acme-stack", style: { gap: "var(--acme-" + (props.gap || "space-2") + ")" } }, props.children);
  }
  function Text(props) {
    return h("span", { className: cx("acme-text", "acme-text-" + (props.size || "m"), props.tone === "muted" && "acme-muted") }, props.children);
  }
  function Button(props) {
    return h("button", { type: "button", className: cx("acme-button", "acme-button-" + (props.variant || "secondary")), onClick: props.onClick }, props.children);
  }
  function Card(props) {
    return h("section", { className: "acme-card" }, props.title ? h("h3", { className: "acme-card-title" }, props.title) : null, props.children);
  }
  return { Root: Root, Stack: Stack, Text: Text, Button: Button, Card: Card };
}
