import dayjs from 'dayjs';
import http from './client';

// 商户端 CSV 导出：/merchant/export/{orders|complaints|profit-sharing}.csv
// 用 axios 取 blob 是为了读到服务端 Content-Disposition 里的文件名与 X-Export-* 头；
// 下载动作走 a[download]，不经过后端页面跳转。

export type MerchantExportKind = 'orders' | 'complaints' | 'profit-sharing';

export interface MerchantExportQuery {
  status?: string;
  keyword?: string;
  payment_type?: string;
  created_from?: string;
  created_to?: string;
}

export interface MerchantExportResult {
  filename: string;
  rows: number;
  truncated: boolean;
}

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

function parseFilename(disposition: string, fallback: string): string {
  if (!disposition) return fallback;
  const encoded = /filename\*=UTF-8''([^;]+)/i.exec(disposition);
  if (encoded?.[1]) {
    try {
      return decodeURIComponent(encoded[1].trim());
    } catch {
      return encoded[1].trim();
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(disposition);
  return plain?.[1]?.trim() || fallback;
}

export async function downloadMerchantCsv(
  kind: MerchantExportKind,
  query: MerchantExportQuery = {},
): Promise<MerchantExportResult> {
  const response = await http.get<Blob>(`/merchant/export/${kind}.csv`, {
    params: clean(query),
    responseType: 'blob',
  });
  const headers = response.headers as Record<string, string | undefined>;
  const disposition = String(headers['content-disposition'] ?? '');
  const fallback = `${kind}-${dayjs().format('YYYYMMDD-HHmmss')}.csv`;
  const filename = parseFilename(disposition, fallback);
  const rows = Number(headers['x-export-rows'] ?? 0) || 0;
  const truncated = String(headers['x-export-truncated'] ?? '') === 'true';

  const url = URL.createObjectURL(response.data);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);

  return { filename, rows, truncated };
}
