import http from './client';

// 扫码登录（公开页）：电脑端申请二维码并轮询，手机端在 /qr-login 确认。
// 手机确认接口属于商户会话，放在 api/merchantQrLogin.ts，避免两边混在一起。

export interface QrTicket {
  ticket: string;
  qr_svg: string;
  expires_at: string;
}

export interface QrPollResult {
  /** pending / confirmed / expired 之类的状态字符串 */
  status: string;
  token?: string;
  expires_at?: string;
  merchant?: { id: string; display_name: string };
}

export const qrLoginApi = {
  async createTicket() {
    const { data } = await http.post<QrTicket>('/auth/qr/ticket');
    return data;
  },
  async poll(ticket: string) {
    const { data } = await http.get<QrPollResult>('/auth/qr/poll', { params: { ticket } });
    return data;
  },
};
