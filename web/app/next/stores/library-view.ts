// What the tour may do to the library screen (HRN-862): the lesson that leads to a demo project sets aside what hides it
// (a search, a filter, the trash, another tab) or copies the demo back. The library registers itself while it is the
// page; the tour calls it from a card's button.

export interface ILibraryTourControl {
  /** Empties the search and the filters, goes to the library's top and out of the trash. */
  clearView: () => void;
  /** Copies back the demo projects the person deleted (into their «Демо» folder), with the view cleared. */
  restoreDemo: () => Promise<void>;
}

let control: ILibraryTourControl | null = null;

/** The library on screen takes the tour's calls until it goes (the returned function). */
export const registerLibraryControl = (next: ILibraryTourControl): (() => void) => {
  control = next;
  return () => {
    if (control === next) control = null;
  };
};

export const libraryControl = (): ILibraryTourControl | null => control;
