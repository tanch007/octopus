import path from 'node:path';
import { execFileSync } from 'node:child_process';
import babel from '@rolldown/plugin-babel';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import { compression, defineAlgorithm } from 'vite-plugin-compression2';

// resolveGitVersion 取最近的 git tag 作为版本号, 与 scripts/build.sh 注入后端的版本号同源。
// 两端版本号必须一致, 否则前端的版本校验会把正常的构建误判成浏览器缓存问题。
function resolveGitVersion(): string {
    try {
        const tag = execFileSync('git', ['describe', '--tags', '--abbrev=0'], {
            cwd: import.meta.dirname,
            stdio: ['ignore', 'pipe', 'ignore'],
        }).toString().trim();
        return tag || 'dev';
    } catch {
        return 'dev'; // 不在 git 仓库里或没有 tag, 与后端 conf.Version 的默认值一致。
    }
}

// 发布流程会显式注入 VITE_APP_VERSION; 其余构建按 git tag 推导, 保持前后端同源。
if (!process.env.VITE_APP_VERSION) {
    process.env.VITE_APP_VERSION = resolveGitVersion();
}

export default defineConfig({
  base: './',
  plugins: [
    react(),
    babel({ presets: [reactCompilerPreset()] }),
    compression({
      algorithms: [defineAlgorithm('gzip', { level: 9 })],
      include: /\.(html|css|js|mjs|json|svg|txt|xml)$/,
      deleteOriginalAssets: true,
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  build: {
    outDir: path.resolve(import.meta.dirname, '../static/out'),
    emptyOutDir: true,
  },
  server: {
    hmr: process.env.DISABLE_HMR !== 'true',
    watch: process.env.DISABLE_HMR === 'true' ? null : {},
    proxy: {
      '/api': {
        target: process.env.VITE_PROXY_TARGET || 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
});
