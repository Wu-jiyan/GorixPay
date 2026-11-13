import { defineStore } from 'pinia';
import { authApi } from '@/api';
import type { Administrator, Merchant, SessionInfo } from '@/api/types';

interface AuthState {
  token: string;
  principalType: 'administrator' | 'merchant' | '';
  merchant: Merchant | null;
  administrator: Administrator | null;
  expiresAt: string;
  loaded: boolean;
}

const PROFILE_KEY = 'gorixpay_profile';

function readProfile(): { merchant: Merchant | null; administrator: Administrator | null } {
  try {
    const raw = localStorage.getItem(PROFILE_KEY);
    if (!raw) return { merchant: null, administrator: null };
    return JSON.parse(raw);
  } catch {
    return { merchant: null, administrator: null };
  }
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => {
    const profile = readProfile();
    return {
      token: localStorage.getItem('gorixpay_token') ?? '',
      principalType: (localStorage.getItem('gorixpay_principal') as AuthState['principalType']) ?? '',
      merchant: profile.merchant,
      administrator: profile.administrator,
      expiresAt: localStorage.getItem('gorixpay_expires') ?? '',
      loaded: false,
    };
  },
  getters: {
    isLoggedIn: (state) => Boolean(state.token),
    displayName: (state) => state.merchant?.display_name || state.administrator?.display_name || '用户',
  },
  actions: {
    persist() {
      localStorage.setItem('gorixpay_token', this.token);
      localStorage.setItem('gorixpay_principal', this.principalType);
      localStorage.setItem('gorixpay_expires', this.expiresAt);
      localStorage.setItem(
        PROFILE_KEY,
        JSON.stringify({ merchant: this.merchant, administrator: this.administrator }),
      );
    },
    setMerchantLogin(
      token: string,
      expiresAt: string,
      merchant: { id: string; display_name: string; legacy_uid?: number | null },
    ) {
      this.token = token;
      this.expiresAt = expiresAt;
      this.principalType = 'merchant';
      this.merchant = {
        id: merchant.id,
        legacy_uid: merchant.legacy_uid ?? null,
        display_name: merchant.display_name,
        status: 'active',
        created_at: new Date().toISOString(),
      };
      this.persist();
    },
    setAdminLogin(token: string, expiresAt: string, administrator: Administrator) {
      this.token = token;
      this.expiresAt = expiresAt;
      this.principalType = 'administrator';
      this.administrator = administrator;
      this.persist();
    },
    async loadSession(): Promise<SessionInfo | null> {
      if (!this.token) {
        this.loaded = true;
        return null;
      }
      try {
        const session = await authApi.me();
        this.principalType = session.principal_type.includes('administrator') ? 'administrator' : 'merchant';
        this.loaded = true;
        return session;
      } catch {
        this.reset();
        this.loaded = true;
        return null;
      }
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
      this.token = '';
      this.principalType = '';
      this.merchant = null;
      this.administrator = null;
      this.expiresAt = '';
      localStorage.removeItem('gorixpay_token');
      localStorage.removeItem('gorixpay_principal');
      localStorage.removeItem('gorixpay_expires');
      localStorage.removeItem(PROFILE_KEY);
    },
  },
});
