import { computed, ref } from 'vue';
import axios from 'axios';
import { formatDateTime } from '@/utils/format';

/** 运营后台页面共用的取值 / 分页工具（仅本目录使用） */
export type Row = Record<string, unknown>;

export function asRow(value: unknown): Row {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? (value as Row) : {};
}

export function hasKeys(value: unknown): boolean {
  return Object.keys(asRow(value)).length > 0;
}

/** 兼容 snake_case / PascalCase / camelCase 三种后端命名 */
export function pick(row: unknown, ...keys: string[]): unknown {
  const record = asRow(row);
  for (const key of keys) {
    const value = record[key];
    if (value !== undefined && value !== null && value !== '') return value;
  }
  return undefined;
}

export function textOf(value: unknown, fallback = '-'): string {
  if (value === undefined || value === null || value === '') return fallback;
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (typeof value === 'object') return previewJson(value);
  return fallback;
}

export function fieldText(row: unknown, ...keys: string[]): string {
  return textOf(pick(row, ...keys));
}

export function fieldDateTime(row: unknown, ...keys: string[]): string {
  const value = pick(row, ...keys);
  return typeof value === 'string' ? formatDateTime(value) : '-';
}

export function fieldNumber(row: unknown, ...keys: string[]): number | null {
  const value = pick(row, ...keys);
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

/** 单元格里展示 JSON / 长文本：截断 */
export function previewJson(value: unknown, max = 120): string {
  if (value === undefined || value === null || value === '') return '-';
  let text: string;
  if (typeof value === 'string') {
    text = value;
  } else {
    try {
      text = JSON.stringify(value) ?? String(value);
    } catch {
      text = String(value);
    }
  }
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

/** 详情弹窗里展示完整 JSON：解析成功则格式化，失败保留原文 */
export function prettyJson(value: unknown): string {
  if (typeof value === 'string') {
    if (value.trim() === '') return '-';
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  if (value === undefined || value === null) return '-';
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}

/** 取 HTTP 状态码，用于识别「接口未启用」这类 404 */
export function httpStatus(error: unknown): number | undefined {
  return axios.isAxiosError(error) ? error.response?.status : undefined;
}

/** 后端可能返回 {items:[...]} 也可能返回裸数组，统一取出列表 */
export function itemsOf<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[];
  const nested = asRow(value).items;
  return Array.isArray(nested) ? (nested as T[]) : [];
}

/** 数字兜底：字段缺失/非法时用 fallback（例如分页 total） */
export function numberOr(value: unknown, fallback = 0): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

/** 后端可能把字符串数组存成 JSON 串或逗号分隔串，统一成 string[] */
export function stringList(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value
      .map((item) => (typeof item === 'string' ? item.trim() : String(item)))
      .filter((item) => item !== '');
  }
  if (typeof value !== 'string') return [];
  const text = value.trim();
  if (text === '') return [];
  if (text.startsWith('[')) {
    try {
      const parsed: unknown = JSON.parse(text);
      if (Array.isArray(parsed)) return stringList(parsed);
    } catch {
      // 解析失败按逗号分隔处理
    }
  }
  return text
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item !== '');
}

export interface LedgerAccountRow {
  id: string;
  owner_type: string;
  owner_id: string;
  purpose: string;
  currency: string;
  status: string;
  available: number;
  frozen: number;
  created_at: string;
}

/** 账本账户：后端可能返回 {account, balance} 嵌套结构，也可能返回扁平结构 */
export function normalizeLedgerAccount(value: unknown): LedgerAccountRow {
  const raw = asRow(value);
  const account = asRow(raw.account);
  const balance = asRow(raw.balance);
  const nested = hasKeys(account);
  const source = nested ? account : raw;
  const available = nested ? balance.available : raw.available;
  const frozen = nested ? balance.frozen : raw.frozen;
  return {
    id: textOf(source.id, ''),
    owner_type: textOf(source.owner_type, ''),
    owner_id: textOf(source.owner_id, ''),
    purpose: textOf(source.purpose, ''),
    currency: textOf(source.currency, ''),
    status: textOf(source.status, ''),
    available: typeof available === 'number' ? available : 0,
    frozen: typeof frozen === 'number' ? frozen : 0,
    created_at: textOf(source.created_at, ''),
  };
}

/** ISO 时间串 → naive-ui 日期选择器可用的时间戳 */
export function toTimestamp(value?: string | null): number | null {
  if (!value) return null;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? null : parsed;
}

/** 日期范围时间戳 → ISO 字符串（后端 created_from / created_to） */
export function toISOString(timestamp?: number | null): string | undefined {
  if (typeof timestamp !== 'number' || Number.isNaN(timestamp)) return undefined;
  return new Date(timestamp).toISOString();
}

/** 服务端分页：统一 page / page_size / itemCount 与事件处理 */
export function useServerPaging(loader: () => unknown, initialSize = 20) {
  const page = ref(1);
  const pageSize = ref(initialSize);
  const total = ref(0);

  const pagination = computed(() => ({
    page: page.value,
    pageSize: pageSize.value,
    itemCount: total.value,
    showSizePicker: true,
    pageSizes: [10, 20, 50, 100],
    prefix: () => `共 ${total.value} 条`,
  }));

  function onPage(value: number) {
    page.value = value;
    void loader();
  }

  function onPageSize(value: number) {
    pageSize.value = value;
    page.value = 1;
    void loader();
  }

  function resetPage() {
    page.value = 1;
  }

  return { page, pageSize, total, pagination, onPage, onPageSize, resetPage };
}
