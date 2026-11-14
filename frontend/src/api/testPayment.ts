import http from './client';

export interface TestOrderResult {
  trade_no?: string;
  pay_type?: string;
  pay_info?: unknown;
  cashier_url?: string;
}

export const testPaymentApi = {
  /** 用指定渠道创建一笔不落业务库的测试订单 */
  async createTestOrder(channelId: string, payload: { payment_type: string; amount_cents: number }) {
    const { data } = await http.post<TestOrderResult>(`/admin/channels/${channelId}/test-order`, payload);
    return data;
  },
};
