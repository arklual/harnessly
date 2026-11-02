declare module 'fast-formula-parser' {
  interface IFormulaOptions {
    onCell?: (ref: never) => unknown;
    onRange?: (ref: never) => unknown;
    onVariable?: (name: string, sheet: string) => unknown;
  }
  interface IPosition {
    sheet: string;
    row: number;
    col: number;
  }
  export class DepParser {
    constructor(options?: IFormulaOptions);
    parse(formula: string, position: IPosition): unknown;
  }
  export default class FormulaParser {
    constructor(options?: IFormulaOptions);
    parse(formula: string, position: IPosition): unknown;
  }
}
