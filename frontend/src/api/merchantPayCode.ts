import http from './client';

// 商户收款码与自助测试支付。
// 收款码依赖公开的支付方式目录 /public/pay-types，这里顺便包一层，避免页面直接拼 URL。

export interface PublicPayType {
  code: string;
  name: string;
  icon?: string | null;
  sort?: number;
}

export interface PayCodeResult {
  code_svg: string;
  url: string;
  expires_at: string;
}

export interface TestOrderResult {
  trade_no: string;
  pay_type: string;
  pay_info: string | Record<string, unknown>;
  cashier_url: string;
}

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

export const merchantPayCodeApi = {
  async payTypes() {
    const { data } = await http.get<{ items: PublicPayType[] }>('/public/pay-types');
    return data.items;
  },
  async payCode(params: { payment_type: string; amount_cents?: number }) {
    const { data } = await http.get<PayCodeResult>('/merchant/pay-code', { params: clean(params) });
    return data;
  },
  async testOrder(payload: { payment_type: string; amount_cents: number }) {
    const { data } = await http.post<TestOrderResult>('/merchant/test-order', payload);
    return data;
  },
};
