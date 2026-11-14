import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

// 依赖分包：把组件库、框架运行时、工具库拆成独立 chunk，
// 避免所有 node_modules 被并进入口 index-*.js（首屏单 chunk 过大）。
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

function manualChunks(id: string): string | undefined {
  if (!id.includes('node_modules')) return undefined;
  // 日期库只被日期选择器用到，单独成块，避免并进组件库主 chunk
  if (id.includes('date-fns')) return 'vendor-date';
  // 表格 / 日期选择器这类重型控件单独成块：体积大但变化少，命中缓存率高
  if (
    id.includes('naive-ui/es/data-table') ||
    id.includes('naive-ui/es/date-picker') ||
    id.includes('naive-ui/es/_internal/date-panel') ||
    id.includes('vueuc')
  ) {
    return 'vendor-naive-widgets';
  }
  if (VENDOR_NAIVE.some((name) => id.includes(name))) return 'vendor-naive';
  if (id.includes('vue-router') || id.includes('pinia')) return 'vendor-vue';
  if (id.includes('/@vue/') || id.includes('/vue/')) return 'vendor-vue';
  if (id.includes('axios')) return 'vendor-axios';
  if (id.includes('dayjs')) return 'vendor-dayjs';
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
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    // 首屏体积靠拆分控制，阈值调回接近默认值，让后续膨胀重新可见
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
});
