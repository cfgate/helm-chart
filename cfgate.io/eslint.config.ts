import js from '@eslint/js'
import { defineConfig } from 'eslint/config'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default defineConfig({
  rules: {
    'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
  },
  languageOptions: {
    globals: {
      ...globals.browser,
      ...globals.node,
      ...globals.serviceworker,
    },
    parser: tseslint.parser,
    parserOptions: {
      ecmaFeatures: {
        impliedStrict: true,
      },
      projectService: true,
      sourceType: 'module',
    },
  },
  plugins: {
    js: js,
    tseslint: tseslint,
  },
  extends: [js.configs.recommended, tseslint.configs.stylisticTypeChecked],
  files: ['src/**'],
  ignores: ['docs/**', 'node_modules/**'],
})
