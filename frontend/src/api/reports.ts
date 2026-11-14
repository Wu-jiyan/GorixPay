import http from './client';
import type { Paginated } from './types';

/** 统计接口的字段名随聚合维度变化，页面按列名做兼容取值 */
export type ReportRow = Record<string, unknown>;

export interface ReportQuery {
  page?: number;
  page_size?: number;
  keyword?: string;
  created_from?: string;
  created_to?: string;
}

/** 导出类型：orders/refunds/settlements/transfers/merchants/ledger 见契约文档；buyers 按 stats/buyers 对称补齐 */
export type ExportKind =
  | 'orders'
  | 'refunds'
  | 'settlements'
  | 'transfers'
  | 'merchants'
  | 'ledger'
  | 'buyers';

export const reportsApi = {
  async buyers(query: ReportQuery = {}) {
    const { data } = await http.get<Paginated<ReportRow> | ReportRow[]>('/admin/stats/buyers', { params: query });
    return data;
  },
  async merchants(query: ReportQuery = {}) {
    const { data } = await http.get<Paginated<ReportRow> | ReportRow[]>('/admin/stats/merchants', {
      params: query,
    });
    return data;
  },
};

/**
 * 拼导出地址：CSV 下载用 window.open 直连后端，不走 axios（避免把二进制塞进 JS 内存）。
 * 同源请求会带上会话 Cookie，导出权限与后台一致。
 */
export function exportCsvUrl(kind: ExportKind, query: ReportQuery = {}): string {
  const base = http.defaults.baseURL || '/api/v1';
  const params = new URLSearchParams();
  Object.entries(query).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return;
    params.set(key, String(value));
  });
  const search = params.toString();
  return `${base}/admin/export/${kind}.csv${search ? `?${search}` : ''}`;
}
