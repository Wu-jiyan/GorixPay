<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import axios from 'axios';
import { useMessage } from 'naive-ui';
import { IdCardOutline, RefreshOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { Certification } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(true);
const saving = ref(false);
const notEnabled = ref(false);
const info = ref<Certification | null>(null);

const form = reactive({
  cert_type: 'person' as 'person' | 'corp',
  real_name: '',
  id_no: '',
  corp_name: '',
  corp_no: '',
});

const status = computed(() => info.value?.status ?? 'none');
const isCorp = computed(() => form.cert_type === 'corp');
const required = computed(() => Boolean(info.value?.required));
const feeCents = computed(() => info.value?.fee_cents ?? 0);

const canSubmit = computed(
  () => !notEnabled.value && !saving.value && status.value !== 'pending' && status.value !== 'approved',
);

const statusMeta = computed(() => {
  switch (status.value) {
    case 'pending':
      return {
        type: 'info' as const,
        title: '审核中',
        desc: '资料已提交，平台运营会尽快完成审核，审核结果将在本页更新。',
      };
    case 'approved':
      return {
        type: 'success' as const,
        title: '已通过实名认证',
        desc: '认证信息只读展示，如需变更请联系平台运营。',
      };
    case 'rejected':
      return {
        type: 'error' as const,
        title: '审核未通过',
        desc: info.value?.rejection_reason || '资料不符合要求，请修改后重新提交。',
      };
    default:
      return {
        type: 'warning' as const,
        title: '未完成实名认证',
        desc: required.value
          ? '平台已开启强制实名认证，未通过认证无法创建支付订单。'
          : '完成实名认证可提升账户额度、结算与提现能力。',
      };
  }
});

const certTypeText = computed(() => {
  const certType = info.value?.cert_type;
  if (certType === 'corp') return '企业';
  if (certType === 'person') return '个人';
  return '-';
});

function validateIdNo(value: string): boolean {
  return /^\d{17}[\dXx]$/.test(value.trim());
}

async function load() {
  loading.value = true;
  try {
    info.value = await merchantApi.certification();
    notEnabled.value = false;
    const certType = info.value?.cert_type;
    if (certType === 'corp' || certType === 'person') {
      form.cert_type = certType;
    }
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      notEnabled.value = true;
      info.value = null;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function handleSubmit() {
  if (!canSubmit.value) return;
  if (!form.real_name.trim()) {
    message.error('请填写真实姓名');
    return;
  }
  if (!validateIdNo(form.id_no)) {
    message.error('请输入 18 位身份证号（最后一位可以是 X）');
    return;
  }
  if (isCorp.value && !form.corp_name.trim()) {
    message.error('企业认证请填写企业名称');
    return;
  }
  if (isCorp.value && !form.corp_no.trim()) {
    message.error('企业认证请填写营业执照号');
    return;
  }
  saving.value = true;
  try {
    await merchantApi.submitCertification({
      cert_type: form.cert_type,
      real_name: form.real_name.trim(),
      id_no: form.id_no.trim(),
      corp_name: isCorp.value ? form.corp_name.trim() : undefined,
      corp_no: isCorp.value ? form.corp_no.trim() : undefined,
    });
    message.success('认证资料已提交，等待平台审核');
    form.real_name = '';
    form.id_no = '';
    form.corp_name = '';
    form.corp_no = '';
    await load();
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 409) {
      message.error('认证已通过或正在审核中，无需重复提交');
      await load();
    } else if (axios.isAxiosError(err) && err.response?.status === 404) {
      message.error('实名认证接口未启用，请联系平台运营');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <admin-page title="实名认证" subtitle="提交个人或企业资料，审核通过后解锁完整收款能力">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新状态
      </n-button>
    </template>

    <n-spin :show="loading">
      <n-alert v-if="notEnabled" type="default" :bordered="false" style="margin-bottom: 16px">
        实名认证接口未启用：当前平台未开放实名认证能力，可稍后再试或联系平台运营。
      </n-alert>

      <n-alert
        v-else
        :type="statusMeta.type"
        :bordered="false"
        :title="statusMeta.title"
        style="margin-bottom: 16px"
      >
        <n-space vertical :size="8">
          <span>{{ statusMeta.desc }}</span>
          <n-space align="center">
            <n-tag size="small" :bordered="false" :type="required ? 'warning' : 'default'">
              {{ required ? '强制认证已开启' : '自愿认证' }}
            </n-tag>
            <n-tag v-if="feeCents > 0" size="small" :bordered="false" type="info">
              认证费 {{ formatMoney(feeCents) }}
            </n-tag>
            <n-tag v-else size="small" :bordered="false" type="success">认证免费</n-tag>
          </n-space>
        </n-space>
      </n-alert>

      <n-grid v-if="status !== 'none'" cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-card size="small" :bordered="false" title="认证信息（脱敏）">
            <n-descriptions :column="1" label-placement="left" :label-width="120" size="small" bordered>
              <n-descriptions-item label="认证类型">{{ certTypeText }}</n-descriptions-item>
              <n-descriptions-item label="证件姓名">{{ info?.real_name_masked || '-' }}</n-descriptions-item>
              <n-descriptions-item label="身份证号">{{ info?.id_no_masked || '-' }}</n-descriptions-item>
              <n-descriptions-item v-if="info?.cert_type === 'corp'" label="企业名称">
                {{ info?.corp_name || '-' }}
              </n-descriptions-item>
              <n-descriptions-item v-if="info?.cert_type === 'corp'" label="营业执照号">
                {{ info?.corp_no || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="提交时间">{{ formatDateTime(info?.submitted_at) }}</n-descriptions-item>
              <n-descriptions-item label="审核时间">{{ formatDateTime(info?.reviewed_at) }}</n-descriptions-item>
              <n-descriptions-item label="认证费用">{{ formatMoney(feeCents) }}</n-descriptions-item>
              <n-descriptions-item v-if="info?.rejection_reason" label="驳回原因">
                <n-text type="error">{{ info.rejection_reason }}</n-text>
              </n-descriptions-item>
            </n-descriptions>
          </n-card>
        </n-gi>

        <n-gi>
          <n-card size="small" :bordered="false" title="说明">
            <n-list>
              <n-list-item>认证信息仅用于平台合规校验，平台按加密方式存储，仅展示脱敏结果。</n-list-item>
              <n-list-item v-if="feeCents > 0">审核通过时会从可用余额扣除认证费，余额不足将无法通过审核。</n-list-item>
              <n-list-item v-if="required">强制认证期间，未通过认证的商户在 V1 / V2 下单都会被拦截。</n-list-item>
              <n-list-item>如资料有误或需要变更，请联系平台运营人工处理。</n-list-item>
            </n-list>
          </n-card>
        </n-gi>
      </n-grid>

      <n-card
        v-if="canSubmit && !notEnabled"
        size="small"
        :bordered="false"
        title="提交认证资料"
        style="margin-top: 16px"
      >
        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
          <n-form-item label="认证类型">
            <n-radio-group v-model:value="form.cert_type">
              <n-radio-button value="person">个人认证</n-radio-button>
              <n-radio-button value="corp">企业认证</n-radio-button>
            </n-radio-group>
          </n-form-item>
          <n-grid cols="1 m:2" responsive="screen" :x-gap="16">
            <n-gi>
              <n-form-item label="真实姓名">
                <n-input v-model:value="form.real_name" placeholder="请输入证件上的真实姓名" :maxlength="30" />
              </n-form-item>
            </n-gi>
            <n-gi>
              <n-form-item label="身份证号">
                <n-input v-model:value="form.id_no" placeholder="18 位身份证号，最后一位可为 X" :maxlength="18" />
              </n-form-item>
            </n-gi>
            <n-gi v-if="isCorp">
              <n-form-item label="企业名称">
                <n-input v-model:value="form.corp_name" placeholder="请输入营业执照上的企业全称" :maxlength="128" />
              </n-form-item>
            </n-gi>
            <n-gi v-if="isCorp">
              <n-form-item label="营业执照号">
                <n-input v-model:value="form.corp_no" placeholder="统一社会信用代码 / 注册号" :maxlength="64" />
              </n-form-item>
            </n-gi>
          </n-grid>
          <n-alert type="info" :bordered="false" style="margin: 4px 0 16px">
            提交后由平台运营审核，请确保姓名、身份证号与证件完全一致，否则会被驳回。
          </n-alert>
          <n-form-item>
            <n-button type="primary" :block="isNarrow" :loading="saving" @click="handleSubmit">
              <template #icon>
                <n-icon :component="IdCardOutline" />
              </template>
              提交认证
            </n-button>
          </n-form-item>
        </n-form>
      </n-card>

      <n-card v-if="status === 'approved'" size="small" :bordered="false" title="后续操作" style="margin-top: 16px">
        <n-text depth="2">认证已通过。若需要变更认证主体，请联系平台运营先撤销当前认证，再重新提交。</n-text>
      </n-card>
    </n-spin>
  </admin-page>
</template>
