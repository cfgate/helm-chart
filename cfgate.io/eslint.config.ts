import js from '@eslint/js'
import { defineConfig } from 'eslint/config'
import tseslint from 'typescript-eslint'

export default defineConfig([
  tseslint.configs.recommended,
  {
    files: ['src/**'],
    languageOptions: {
      globals: {
        // TODO: configure Cloudflare Workers globals? Include ./worker-configuration.d.ts?
      },
    },
    plugins: {
      js,
    },
    extends: ['js/recommended'],
  },
])
