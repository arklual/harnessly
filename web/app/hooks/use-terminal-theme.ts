// The terminal colour theme the person picked on another page; the agents page only remembers the name.
let themeName: string | null = null;

export const initTerminalTheme = (name: string): void => {
  themeName = name;
};

export const terminalThemeName = (): string | null => themeName;
