import dayjs from 'dayjs';

export function formatMoney(cents?: number | null, withSymbol = true): string {
  const value = typeof cents === 'number' ? cents / 100 : 0;
  const text = value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return withSymbol ? `¥${text}` : text;
}

export function yuanToCents(yuan: string | number): number {
  const value = typeof yuan === 'number' ? yuan : Number.parseFloat(String(yuan));
  if (!Number.isFinite(value)) return 0;
  return Math.round(value * 100);
}

export function centsToYuan(cents: number): string {
  return (cents / 100).toFixed(2);
}

export function formatDateTime(value?: string | null): string {
  if (!value) return '-';
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.format('YYYY-MM-DD HH:mm:ss') : '-';
}

export function formatDate(value?: string | null): string {
  if (!value) return '-';
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.format('YYYY-MM-DD') : '-';
}

const orderStatusMap: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待支付', type: 'warning' },
  processing: { text: '支付中', type: 'info' },
  paid: { text: '已支付', type: 'success' },
  closed: { text: '已关闭', type: 'default' },
  failed: { text: '失败', type: 'error' },
};

const financeStatusMap: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待审核', type: 'warning' },
  approved: { text: '已通过', type: 'success' },
  rejected: { text: '已驳回', type: 'error' },
  processing: { text: '处理中', type: 'info' },
  paid: { text: '已支付', type: 'success' },
  failed: { text: '失败', type: 'error' },
};

export function orderStatus(status?: string) {
  return orderStatusMap[status ?? ''] ?? { text: status || '-', type: 'default' as const };
}

export function financeStatus(status?: string) {
  return financeStatusMap[status ?? ''] ?? { text: status || '-', type: 'default' as const };
}

export function activeStatus(status?: string) {
  if (status === 'active') return { text: '启用', type: 'success' as const };
  if (status === 'disabled') return { text: '禁用', type: 'default' as const };
  return { text: status || '-', type: 'default' as const };
}

export function paymentTypeText(type?: string): string {
  const map: Record<string, string> = {
    alipay: '支付宝',
    wxpay: '微信支付',
    qqpay: 'QQ 钱包',
    bank: '网银',
    cashier: '收银台',
    usdt: 'USDT',
    sandbox: '沙箱支付',
  };
  return map[type ?? ''] ?? type ?? '-';
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function downloadJSON(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
