import { defineStore } from 'pinia';
import { authApi, merchantApi } from '@/api';
import type { Administrator, Merchant, SessionInfo } from '@/api/types';

// 会话约定（审计 P0/P1）：
// - 浏览器只依赖后端下发的 HttpOnly cookie，前端不保存 token、不发 Authorization；
// - localStorage 只留展示用的商户/管理员资料，绝不作为鉴权依据；
// - 身份一律以 GET /auth/me 的结果为准，应用启动时校验一次。

interface AuthState {
  principalType: 'administrator' | 'merchant' | '';
  merchant: Merchant | null;
  administrator: Administrator | null;
  session: SessionInfo | null;
  /** 是否已经向服务端确认过会话（成功或失败都算确认） */
  sessionChecked: boolean;
}

const PROFILE_KEY = 'gorixpay_profile';
const LEGACY_KEYS = ['gorixpay_token', 'gorixpay_principal', 'gorixpay_expires'];

type MerchantIdentity = { id: string; display_name: string; legacy_uid?: number | null };

function readProfile(): { merchant: Merchant | null; administrator: Administrator | null } {
  try {
    const raw = localStorage.getItem(PROFILE_KEY);
    if (!raw) return { merchant: null, administrator: null };
    const parsed = JSON.parse(raw) as { merchant?: Merchant | null; administrator?: Administrator | null };
    return { merchant: parsed.merchant ?? null, administrator: parsed.administrator ?? null };
  } catch {
    return { merchant: null, administrator: null };
  }
}

/** 同时发起的 ensureSession 只发一次 /auth/me */
let sessionRequest: Promise<SessionInfo | null> | null = null;
let sessionRequestKey: symbol | null = null;
/** 登出/401 会递增；用于丢弃登出前发出的 /auth/me 响应，避免登出后又被写回登录态 */
let sessionEpoch = 0;

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => {
    const profile = readProfile();
    return {
      principalType: '',
      merchant: profile.merchant,
      administrator: profile.administrator,
      session: null,
      sessionChecked: false,
    };
  },
  getters: {
    isLoggedIn: (state) => state.sessionChecked && state.principalType !== '',
    isAdministrator: (state) => state.principalType === 'administrator',
    isMerchant: (state) => state.principalType === 'merchant',
    displayName: (state) => state.merchant?.display_name || state.administrator?.display_name || '用户',
  },
  actions: {
    /** 只持久化展示资料；token 等敏感值一律不落盘 */
    persist() {
      this.clearLegacyStorage();
      localStorage.setItem(
        PROFILE_KEY,
        JSON.stringify({ merchant: this.merchant, administrator: this.administrator }),
      );
    },
    clearLegacyStorage() {
      LEGACY_KEYS.forEach((key) => localStorage.removeItem(key));
    },
    setMerchantLogin(merchant: MerchantIdentity) {
      sessionEpoch += 1;
      sessionRequest = null;
      sessionRequestKey = null;
      this.principalType = 'merchant';
      this.administrator = null;
      this.session = null;
      this.merchant = {
        id: merchant.id,
        legacy_uid: merchant.legacy_uid ?? null,
        display_name: merchant.display_name,
        status: 'active',
        created_at: new Date().toISOString(),
      };
      this.sessionChecked = true;
      this.persist();
    },
    setAdminLogin(administrator: Administrator) {
      sessionEpoch += 1;
      sessionRequest = null;
      sessionRequestKey = null;
      this.principalType = 'administrator';
      this.merchant = null;
      this.session = null;
      this.administrator = administrator;
      this.sessionChecked = true;
      this.persist();
    },
    /**
     * 首次导航时调用：以 /auth/me 为准恢复身份。
     * 失败（cookie 过期/无会话/接口异常）一律当作未登录。
     */
    async ensureSession(): Promise<SessionInfo | null> {
      if (this.sessionChecked) return this.session;
      if (sessionRequest) return sessionRequest;
      const epoch = sessionEpoch;
      const key = Symbol('auth-session');
      const request = (async () => {
        try {
          const session = await authApi.me();
          if (epoch !== sessionEpoch) return null;
          this.session = session;
          this.principalType = session.principal_type.includes('administrator') ? 'administrator' : 'merchant';
          this.sessionChecked = true;
          if (this.principalType === 'administrator') {
            this.merchant = null;
            if (!this.administrator) {
              this.administrator = { id: session.principal_id, username: '', display_name: '管理员' };
            }
          } else {
            this.administrator = null;
            if (!this.merchant) {
              try {
                const profile = await merchantApi.profile();
                if (epoch !== sessionEpoch) return null;
                this.merchant = { ...profile };
              } catch {
                this.merchant = {
                  id: session.principal_id,
                  display_name: '商户',
                  status: 'active',
                  created_at: '',
                };
              }
            }
          }
          if (epoch !== sessionEpoch) return null;
          this.persist();
          return session;
        } catch {
          // 会话已失效：清空本地状态。若期间已经登出，就不再重复 reset
          if (epoch === sessionEpoch) this.reset();
          return null;
        } finally {
          if (sessionRequestKey === key) {
            sessionRequest = null;
            sessionRequestKey = null;
          }
        }
      })();
      sessionRequest = request;
      sessionRequestKey = key;
      return request;
    },
    /** 扫码登录确认后强制重新校验 cookie 会话 */
    async refreshSession(): Promise<SessionInfo | null> {
      sessionEpoch += 1;
      sessionRequest = null;
      sessionRequestKey = null;
      this.sessionChecked = false;
      this.session = null;
      this.principalType = '';
      return this.ensureSession();
    },
    async logout() {
      try {
        await authApi.logout();
      } catch {
        // 忽略登出接口错误，本地状态仍需清理
      }
      this.reset();
    },
    reset() {
      sessionEpoch += 1;
      sessionRequest = null;
      sessionRequestKey = null;
      this.principalType = '';
      this.merchant = null;
      this.administrator = null;
      this.session = null;
      // 已确认过「当前无会话」，避免登录页/守卫反复请求 /auth/me
      this.sessionChecked = true;
      this.clearLegacyStorage();
      localStorage.removeItem(PROFILE_KEY);
    },
  },
});
