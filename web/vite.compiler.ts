import babel from '@rolldown/plugin-babel';
import { reactCompilerPreset } from '@vitejs/plugin-react';

/**
 * The React Compiler memoizes the components and the hooks: a parent that draws again no longer draws the
 * children whose props did not change, and handlers keep their identity without useCallback. viz/ drives canvas engines
 * imperatively (its lint keeps the compiler's rules off), so it is left as written; node_modules too.
 */
export const reactCompiler = () => {
  const preset = reactCompilerPreset();
  preset.rolldown.filter = {
    ...preset.rolldown.filter,
    id: { exclude: [/[\\/]node_modules[\\/]/, /[\\/]web[\\/]viz[\\/]/] },
  };
  return babel({ presets: [preset] });
};
