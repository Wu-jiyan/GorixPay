import { createApp } from 'vue';
import type { Component } from 'vue';
import { createPinia } from 'pinia';
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NConfigProvider,
  NDataTable,
  NDatePicker,
  NDescriptions,
  NDescriptionsItem,
  NDialogProvider,
  NDivider,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NEmpty,
  NForm,
  NFormItem,
  NGi,
  NGrid,
  NInput,
  NInputNumber,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NMessageProvider,
  NModal,
  NNotificationProvider,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NResult,
  NSelect,
  NSpace,
  NSpin,
  NStep,
  NSteps,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NTimeline,
  NTimelineItem,
} from 'naive-ui';
import App from './App.vue';
import router from './router';
import './styles/global.css';

const app = createApp(App);
app.use(createPinia());
app.use(router);

// 按需注册：只注册模板里实际出现的组件（n-* 标签），
// 不再 app.use(naive) 把整套组件库打进首屏。
// 页面里通过 h(XX) 渲染的组件各自 import，不依赖这里的全局注册。
const naiveComponents: Record<string, Component> = {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NConfigProvider,
  NDataTable,
  NDatePicker,
  NDescriptions,
  NDescriptionsItem,
  NDialogProvider,
  NDivider,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NEmpty,
  NForm,
  NFormItem,
  NGi,
  NGrid,
  NInput,
  NInputNumber,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NMessageProvider,
  NModal,
  NNotificationProvider,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NResult,
  NSelect,
  NSpace,
  NSpin,
  NStep,
  NSteps,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NTimeline,
  NTimelineItem,
};

Object.entries(naiveComponents).forEach(([name, component]) => {
  app.component(name, component);
});

app.mount('#app');
