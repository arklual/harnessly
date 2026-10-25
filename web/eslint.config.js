import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'test-results', 'playwright-report'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: { ecmaVersion: 2023, globals: { ...globals.browser, ...globals.node } },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },
  {
    // The agents page parses terminal output (control characters on purpose), keeps Russian copy with no-break spaces
    // and swallows failures of best-effort browser APIs (storage, clipboard) in empty catch blocks.
    files: ['app/**/*.{ts,tsx}'],
    rules: {
      'no-control-regex': 'off',
      'no-irregular-whitespace': [
        'error',
        { skipStrings: true, skipTemplates: true, skipComments: true, skipJSXText: true },
      ],
      'no-empty': ['error', { allowEmptyCatch: true }],
      'no-useless-assignment': 'off',
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // The new interface stands on its own (spec §6): from the current one only @/platform/{config,router,theme,head},
    // and none of the libraries it replaces. app/next/legacy is the one bridge to the current interface's pane tools,
    // and goes when they are written for the new one.
    files: ['app/next/**/*.{ts,tsx}'],
    ignores: ['app/next/legacy/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            'zustand',
            'swr',
            'sonner',
            'socket.io-client',
            'motion',
            'lucide-react',
            'react-icons',
            'cmdk',
          ].map((name) => ({ name, message: 'The new interface does not use this library (spec §6).' })),
          patterns: [
            {
              regex: '^@/(?!next(/|$)|platform/(config|router|theme|head)$)',
              message:
                'The new interface imports only @/next/* and @/platform/{config,router,theme,head} (spec §6).',
            },
            {
              group: ['@kit', '@kit/*'],
              message: 'The new interface has its own kit in @/next/ui (spec §6).',
            },
            {
              group: ['zustand/*', 'swr/*', 'motion/*', 'react-icons/*', 'socket.io-client/*'],
              message: 'The new interface does not use this library (spec §6).',
            },
          ],
        },
      ],
    },
  },
);
