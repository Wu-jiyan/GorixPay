/**
 * 商户中心页面共用的展示层工具（仅本目录使用）。
 * 窄屏开关只有一份实现，放在 admin/shared.ts，这里重新导出，
 * 避免商户端再抄一遍 matchMedia。
 */
export { useNarrow } from '../admin/shared';