// A small ring that turns while a control waits for its answer; it shows only inside an element with data-busy="true",
// so a button keeps its width when it starts waiting.
const Spinner = () => <span className="hx-spin" aria-hidden="true" />;

export default Spinner;
