import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

// 依赖分包：把组件库、框架运行时、工具库拆成独立 chunk，
// 避免所有 node_modules 被并进入口 index-*.js（首屏单 chunk 过大）。
//
// 注意：naive-ui 及其全部依赖必须落在**同一个** chunk 里。曾经把
// es/data-table、es/date-picker、vueuc 单独拆成 vendor-naive-widgets，结果两个
// chunk 互相 import，形成环，浏览器启动时报
// "Uncaught ReferenceError: Cannot access 'X' before initialization"，
// 页面直接白屏（构建不报错，只有真浏览器能发现）。宁可单块大一点，也不能成环。
const VENDOR_NAIVE = [
  'naive-ui',
  'vueuc',
  'seemly',
  'css-render',
  '@css-render',
  'treemate',
  'vdirs',
  'vooks',
  'evtd',
  '@vicons',
];

function inPackage(id: string, name: string): boolean {
  return id.includes(`/node_modules/${name}/`);
}

function manualChunks(id: string): string | undefined {
  if (!id.includes('node_modules')) return undefined;
  // naive-ui 全家桶（含它内部的 data-table / date-picker / vueuc）一起成块
  if (VENDOR_NAIVE.some((name) => inPackage(id, name))) return 'vendor-naive';
  // 日期库只被日期选择器用到，单向依赖 naive，单独成块不会成环
  if (inPackage(id, 'date-fns')) return 'vendor-date';
  if (inPackage(id, 'vue-router') || inPackage(id, 'pinia')) return 'vendor-vue';
  if (inPackage(id, 'vue') || inPackage(id, '@vue')) return 'vendor-vue';
  if (inPackage(id, 'axios')) return 'vendor-axios';
  if (inPackage(id, 'dayjs')) return 'vendor-dayjs';
  return 'vendor-misc';
}

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        // 与服务端 supervisor 里配置的监听端口保持一致
        target: 'http://127.0.0.1:18088',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    // naive-ui 单块约 900 kB（gzip ~240 kB），成环比大块危险得多，阈值放到 1000 让
    // 「又变回一个 1.6 MB 的入口」这种退化仍然能被警告抓到
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
});
