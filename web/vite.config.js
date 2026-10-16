import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'
import tailwindcss from '@tailwindcss/vite'

// The built assets are embedded by web/embed.go and served by internal/server.
// Everything is bundled locally; the page never references an external CDN.
export default defineConfig({
  plugins: [preact(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    target: 'es2020',
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.js'],
  },
})
