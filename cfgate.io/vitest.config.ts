import { defineWorkersConfig } from '@cloudflare/vitest-pool-workers/config'

export default defineWorkersConfig({
  esbuild: {
    exclude: ['node_modules', 'docs'],
  },
  test: {
    poolOptions: {
      workers: {
        wrangler: { configPath: './wrangler.toml' },
        // miniflare: {
        //   kvNamespaces: ["TEST_NAMESPACE"],
        // },
      },
    },
  },
})
