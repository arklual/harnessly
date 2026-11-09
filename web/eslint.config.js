import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';
import tseslint from 'typescript-eslint';

// What the new interface may import (spec §6); the Sinopia layer below adds its own boundary to the same list.
const newInterfaceImports = {
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
      regex: '^@/(?!next(/|$)|platform/(config|router|theme|head|languages)$)',
      message:
        'The new interface imports only @/next/* and @/platform/{config,router,theme,head,languages} (spec §6).',
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
};

export default tseslint.config(
  { ignores: ['dist', 'dist-hub', 'node_modules', 'test-results', 'playwright-report'] },
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
    // The chart module drives canvas engines imperatively (refs read in effects, engine state kept outside React,
    // renders skipped on pointer moves), so the React Compiler purity rules do not apply to it.
    files: ['viz/**/*.{ts,tsx}', 'e2e/parity/**/*.{ts,tsx}'],
    rules: {
      'react-hooks/refs': 'off',
      'react-hooks/immutability': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/preserve-manual-memoization': 'off',
      'react-refresh/only-export-components': 'off',
      'no-irregular-whitespace': ['error', { skipStrings: true, skipTemplates: true, skipComments: true }],
    },
  },
  {
    // The new interface stands on its own (spec §6): from the current one only @/platform/{config,router,theme,head,languages},
    // and none of the libraries it replaces. app/next/legacy is the one bridge to the current interface's pane tools,
    // and goes when they are written for the new one.
    files: ['app/next/**/*.{ts,tsx}'],
    ignores: ['app/next/legacy/**'],
    rules: {
      'no-restricted-imports': ['error', newInterfaceImports],
    },
  },
  {
    // Sinopia is a builtin app: the editor (features/designly, app-views/sinopia) reaches the host through its
    // SDK in the page — the chat, toasts, keys and windows go through app-views/sinopia/host.ts and its adapters in
    // features/designly/sdk — not through the host's modules. Extend the list as more of the editor moves.
    files: ['app/next/features/app-views/sinopia/**/*.{ts,tsx}', 'app/next/features/designly/**/*.{ts,tsx}'],
    ignores: [
      'app/next/features/app-views/sinopia/host.ts',
      'app/next/features/designly/sdk/**',
      // The guests' page is a page of its own: it mounts the host's toast area as the shell does.
      'app/next/features/designly/guest/guest-app.tsx',
      'app/next/features/app-views/sinopia/**/*.test.{ts,tsx}',
      'app/next/features/designly/**/*.test.{ts,tsx}',
    ],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          ...newInterfaceImports,
          paths: [
            ...newInterfaceImports.paths,
            ...[
              ['@/next/commands/messages', 'the chat: hl.chat.send (app-views/sinopia/host.ts)'],
              ['@/next/ui/toast', 'toasts: designly/sdk/toast (hl.ui.toast)'],
              ['@/next/features/shell/modal', 'windows: designly/sdk/ui'],
              ['@/next/features/keys/commands', 'keys: designly/sdk/keys (hl.keys)'],
              ['@/next/features/keys/use-keys', 'keys: designly/sdk/keys (hl.keys)'],
              ['@/next/features/keys/keys-help', 'keys: designly/sdk/keys (hl.keys.hints)'],
            ].map(([name, instead]) => ({
              name,
              message: `Sinopia reaches the host through its SDK, as a builtin app: ${instead}.`,
            })),
          ],
        },
      ],
    },
  },
);
