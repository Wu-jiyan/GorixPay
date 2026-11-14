<script setup lang="ts">
import { useRouter } from 'vue-router';
import { siteLogo, siteName } from '@/stores/site';

const router = useRouter();
</script>

<template>
  <header class="public-nav">
    <div class="nav-inner">
      <router-link to="/" class="brand">
        <img v-if="siteLogo" :src="siteLogo" class="brand-logo" alt="" />
        <span v-else class="brand-mark">G</span>
        <span class="brand-name">{{ siteName }}</span>
      </router-link>
      <nav class="nav-links">
        <router-link to="/" class="nav-link">首页</router-link>
        <router-link to="/doc" class="nav-link">开发文档</router-link>
        <a class="nav-link" href="/admin/login">运营入口</a>
      </nav>
      <div class="nav-actions">
        <n-button quaternary @click="router.push('/login')">商户登录</n-button>
        <n-button type="primary" @click="router.push('/register')">免费注册</n-button>
      </div>
    </div>
  </header>
</template>

<style scoped>
.public-nav {
  position: sticky;
  top: 0;
  z-index: 20;
  background: rgba(255, 255, 255, 0.92);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid #eef2f7;
}
.nav-inner {
  max-width: 1160px;
  margin: 0 auto;
  height: 64px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  gap: 28px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 700;
  font-size: 18px;
}
.brand-mark {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
}
.brand-logo {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  object-fit: cover;
  display: block;
  flex: none;
}
.nav-links {
  display: flex;
  gap: 20px;
  flex: 1;
}
.nav-link {
  color: #475569;
  font-size: 14px;
}
.nav-link.router-link-active {
  color: #2563eb;
  font-weight: 600;
}
.nav-actions {
  display: flex;
  gap: 8px;
}
@media (max-width: 720px) {
  .nav-links {
    display: none;
  }
  .nav-inner {
    justify-content: space-between;
    gap: 8px;
  }
}
</style>
