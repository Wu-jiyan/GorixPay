import { computed, ref } from 'vue';
import { publicApi } from '@/api';
import type { SiteInfo } from '@/api/types';

// 站点身份（名称、Logo、页脚）由后台「系统设置」的 site.* 决定。
// 公开页不能因为接口失败就空白，所以先给默认值，拉到再覆盖。
const DEFAULT_NAME = 'GorixPay';

const state = ref<SiteInfo>({
  name: '',
  logo_url: '',
  footer_text: '',
  payment_types: [],
});

let pending: Promise<void> | null = null;
let lastPageTitle = '';

export const siteInfo = state;
export const siteName = computed(() => state.value.name || DEFAULT_NAME);
export const siteLogo = computed(() => state.value.logo_url || '');
export const siteFooter = computed(() => state.value.footer_text || '');
export const siteSkin = computed(() => state.value.skin || 'default');

function applySkin() {
  if (typeof document === 'undefined') return;
  document.documentElement.dataset.skin = siteSkin.value;
}

/** 站点身份只拉一次，多个页面同时挂载也只发一个请求。失败不缓存，下次还能重试。 */
export function loadSite(): Promise<void> {
  if (pending) return pending;
  pending = publicApi
    .site()
    .then((site) => {
      state.value = {
        name: site?.name || '',
        logo_url: site?.logo_url || '',
        footer_text: site?.footer_text || '',
        payment_types: site?.payment_types ?? [],
        announcement: site?.announcement ?? null,
      };
      applySkin();
      if (lastPageTitle) setPageTitle(lastPageTitle);
    })
    .catch(() => {
      pending = null;
    });
  return pending;
}

/** 标题统一走这里，站点名换了之后下一次跳转就跟着变。 */
export function setPageTitle(page: string) {
  lastPageTitle = page;
  document.title = page ? `${page} - ${siteName.value}` : `${siteName.value} 支付系统`;
}
