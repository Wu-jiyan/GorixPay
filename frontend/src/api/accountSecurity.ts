import http from './client';
import type { LoginResult } from './types';

// 账户安全：找回密码、修改密码、图形验证码。
// 这些接口都不在 api/index.ts 里（约定：新 API 只写自己的文件）。

export interface CaptchaChallenge {
  /** 图形验证码 id，优先取响应头 X-Captcha-Id，其次是响应体的 id */
  id: string;
  /** 后端返回的 SVG 字符串，登录页用 v-html 渲染 */
  svg: string;
}

export const accountSecurityApi = {
  /** 发送重置邮件；无论账号是否存在后端都返回 202，不返回 token */
  async forgotPassword(identifier: string) {
    await http.post('/auth/merchant/password/forgot', { identifier });
  },
  /** 用邮件里的 token 重置密码，成功 204 */
  async resetPassword(payload: { identifier: string; token: string; password: string }) {
    await http.post('/auth/merchant/password/reset', payload);
  },
  /** 已登录状态修改密码，成功 204 */
  async changePassword(payload: { old_password: string; new_password: string }) {
    await http.post('/merchant/password/change', payload);
  },
  /** 图形验证码：后端未开启时返回 404/400，调用方应静默隐藏 */
  async captcha(): Promise<CaptchaChallenge> {
    const response = await http.get<{ id?: string; image_svg?: string; image?: string }>('/auth/captcha');
    const headers = response.headers ?? {};
    const headerId = headers['x-captcha-id'];
    const data = response.data ?? {};
    const headerValue = Array.isArray(headerId) ? headerId[0] : headerId;
    return {
      id: (typeof headerValue === 'string' && headerValue) || data.id || '',
      svg: data.image_svg || data.image || '',
    };
  },
  /** 带图形验证码的商户登录（验证码未开启时后端忽略这两个字段） */
  async loginMerchantWithCaptcha(
    identifier: string,
    password: string,
    captchaId?: string,
    captchaCode?: string,
  ) {
    const { data } = await http.post<LoginResult>('/auth/merchant/login', {
      identifier,
      password,
      captcha_id: captchaId || undefined,
      captcha_code: captchaCode || undefined,
    });
    return data;
  },
};
