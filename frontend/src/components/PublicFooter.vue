<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NSpace, NText } from 'naive-ui';
import BrandMark from './BrandMark.vue';
import { siteFooter, siteName } from '@/stores/site';

// 官网统一页脚。文案取后台「站点设置 → 页脚」，没填就用默认描述。
const router = useRouter();
const footerText = computed(() => siteFooter.value || '聚合支付 · 兼容易支付 V1 / V2 协议');
const year = new Date().getFullYear();
</script>

<template>
  <footer class="public-footer">
    <div class="gp-container">
      <div class="foot-main">
        <n-space align="center" :size="10">
          <brand-mark :size="26" :name="false" />
          <n-text depth="2" style="font-size: 13px">{{ siteName }}</n-text>
          <n-text depth="3" class="gp-hint">{{ footerText }}</n-text>
        </n-space>
        <n-space :size="16">
          <n-button text @click="router.push('/doc')">开发文档</n-button>
          <n-button text @click="router.push('/login')">商户登录</n-button>
          <n-button text @click="router.push('/register')">免费注册</n-button>
          <n-button text @click="router.push('/admin/login')">运营入口</n-button>
        </n-space>
      </div>
      <n-text depth="3" class="copyright">© {{ year }} {{ siteName }}</n-text>
    </div>
  </footer>
</template>

<style scoped>
.public-footer {
  border-top: 1px solid var(--gp-line);
  background: var(--gp-surface);
  /* 页脚底部留白给足，别让版权行贴着屏幕底边 */
  padding: 28px 0 32px;
}
.foot-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 14px;
}
.copyright {
  display: block;
  margin-top: 14px;
  font-size: 12px;
}
</style>
