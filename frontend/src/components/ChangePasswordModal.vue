<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { NButton, NForm, NFormItem, NInput, NModal, NSpace, NText, useMessage } from 'naive-ui';
import { accountSecurityApi } from '@/api/accountSecurity';
import { securityApi } from '@/api/security';
import { errorMessage } from '@/api/client';

// 改密码只有一个实现：后台顶栏、商户中心顶栏、安全设置页都开这个弹窗。
// 校验放在这里，页面里不再各写一份「至少 8 位、两次要一致」。
const props = withDefaults(
  defineProps<{
    show: boolean;
    /** admin 走 /admin/password/change，merchant 走 /merchant/password/change */
    scope?: 'admin' | 'merchant';
  }>(),
  { scope: 'admin' },
);

const emit = defineEmits<{
  (event: 'update:show', value: boolean): void;
  (event: 'done'): void;
}>();

const message = useMessage();
const busy = ref(false);
const form = ref({ old_password: '', new_password: '', confirm: '' });

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
});

// 关掉就把输入清空：密码不该在内存里多留一份
watch(
  () => props.show,
  (opened) => {
    if (!opened) form.value = { old_password: '', new_password: '', confirm: '' };
  },
);

function runeLength(value: string) {
  return Array.from(value).length;
}

async function submit() {
  const current = form.value;
  if (current.old_password === '') {
    message.warning('请输入当前密码');
    return;
  }
  if (runeLength(current.new_password) < 8 || runeLength(current.new_password) > 72) {
    message.warning('新密码长度需为 8 到 72 个字符');
    return;
  }
  if (current.new_password !== current.confirm) {
    message.warning('两次输入的新密码不一致');
    return;
  }
  if (current.new_password === current.old_password) {
    message.warning('新密码不能与当前密码相同');
    return;
  }
  busy.value = true;
  try {
    const payload = { old_password: current.old_password, new_password: current.new_password };
    if (props.scope === 'merchant') await accountSecurityApi.changePassword(payload);
    else await securityApi.changePassword(payload);
    message.success('密码已修改，其它设备上的登录已失效');
    visible.value = false;
    emit('done');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <n-modal
    v-model:show="visible"
    preset="card"
    title="修改登录密码"
    style="width: 460px; max-width: 92vw"
    :mask-closable="!busy"
    :close-on-esc="!busy"
  >
    <n-form label-placement="top" @submit.prevent="submit">
      <n-form-item label="当前密码">
        <n-input
          v-model:value="form.old_password"
          type="password"
          show-password-on="click"
          placeholder="输入现在使用的密码"
          autocomplete="current-password"
        />
      </n-form-item>
      <n-form-item label="新密码">
        <n-input
          v-model:value="form.new_password"
          type="password"
          show-password-on="click"
          placeholder="8 到 72 个字符"
          autocomplete="new-password"
        />
      </n-form-item>
      <n-form-item label="确认新密码">
        <n-input
          v-model:value="form.confirm"
          type="password"
          show-password-on="click"
          placeholder="再输入一次新密码"
          autocomplete="new-password"
          @keyup.enter="submit"
        />
      </n-form-item>
      <n-text depth="3" style="font-size: 12.5px">改完以后，其它设备上的登录会立即失效，当前这台不受影响。</n-text>
    </n-form>

    <template #footer>
      <n-space justify="end" :size="10">
        <n-button :disabled="busy" @click="visible = false">取消</n-button>
        <n-button type="primary" :loading="busy" @click="submit">保存新密码</n-button>
      </n-space>
    </template>
  </n-modal>
</template>
