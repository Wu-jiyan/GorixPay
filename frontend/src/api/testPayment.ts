import http from './client';

// 后台「测试支付」页：用指定渠道创建一笔测试订单，验证上游参数是否配通。
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

// 官网「支付测试」页（无需登录），对应上游 Epay 首页的 test.php。
// 接口是公开的，能不能用、能用哪些方式、最大金额全部由后端决定。
export interface TestMethod {
  code: string;
  name: string;
}

export interface TestPageConfig {
  enabled: boolean;
  reason: string;
  methods: TestMethod[];
  min_cents: number;
  max_cents: number;
  hourly_limit: number;
  merchant: string;
}

export interface PublicTestOrder {
  trade_no: string;
  payment_type: string;
  amount: number;
  pay_type: string;
  pay_info: unknown;
  /** 站内支付页（/pay/{func}/{trade_no}/），有值时直接跳过去，不再自行解析二维码 */
  pay_page?: string;
  cashier_url: string;
  expires_at: string;
}

export const publicTestApi = {
  async page() {
    const { data } = await http.get<TestPageConfig>('/public/test-payment');
    return data;
  },
  async create(payload: { payment_type: string; amount_cents: number }) {
    const { data } = await http.post<PublicTestOrder>('/public/test-payment', payload);
    return data;
  },
};