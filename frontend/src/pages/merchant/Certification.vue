<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import axios from 'axios';
import { useMessage } from 'naive-ui';
import { merchantApi } from '@/api';
import type { Certification } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime } from '@/utils/format';

const message = useMessage();

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
  <div class="page">
    <PageHeader title="实名认证" subtitle="提交个人或企业资料，审核通过后解锁完整收款能力">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新状态</n-button>
      </template>
    </PageHeader>

    <n-spin :show="loading">
      <n-alert type="default" :bordered="false" class="tips-alert" v-if="notEnabled">
        实名认证接口未启用：当前平台未开放实名认证能力，可稍后再试或联系平台运营。
      </n-alert>

      <n-alert v-else :type="statusMeta.type" :bordered="false" class="status-alert" :title="statusMeta.title">
        <div class="status-line">{{ statusMeta.desc }}</div>
        <div class="status-tags">
          <n-tag size="small" :bordered="false" :type="required ? 'warning' : 'default'">
            {{ required ? '强制认证已开启' : '自愿认证' }}
          </n-tag>
          <n-tag v-if="feeCents > 0" size="small" :bordered="false" type="info">
            认证费 <MoneyText :amount="feeCents" />
          </n-tag>
          <n-tag v-else size="small" :bordered="false" type="success">认证免费</n-tag>
        </div>
      </n-alert>

      <div v-if="status !== 'none'" class="detail-grid">
        <n-card :bordered="false" class="panel-card" title="认证信息（脱敏）">
          <n-descriptions :column="1" label-placement="left" size="small" bordered>
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
            <n-descriptions-item label="认证费用">
              <MoneyText :amount="feeCents" />
            </n-descriptions-item>
            <n-descriptions-item v-if="info?.rejection_reason" label="驳回原因">
              <span class="reject-text">{{ info.rejection_reason }}</span>
            </n-descriptions-item>
          </n-descriptions>
        </n-card>

        <n-card :bordered="false" class="panel-card" title="说明">
          <ul class="note-list">
            <li>认证信息仅用于平台合规校验，平台按加密方式存储，仅展示脱敏结果。</li>
            <li v-if="feeCents > 0">审核通过时会从可用余额扣除认证费，余额不足将无法通过审核。</li>
            <li v-if="required">强制认证期间，未通过认证的商户在 V1 / V2 下单都会被拦截。</li>
            <li>如资料有误或需要变更，请联系平台运营人工处理。</li>
          </ul>
        </n-card>
      </div>

      <n-card v-if="canSubmit && !notEnabled" :bordered="false" class="panel-card form-card" title="提交认证资料">
        <n-form label-placement="top">
          <n-form-item label="认证类型">
            <n-radio-group v-model:value="form.cert_type">
              <n-radio-button value="person">个人认证</n-radio-button>
              <n-radio-button value="corp">企业认证</n-radio-button>
            </n-radio-group>
          </n-form-item>
          <div class="form-row">
            <n-form-item label="真实姓名" class="form-col">
              <n-input v-model:value="form.real_name" placeholder="请输入证件上的真实姓名" :maxlength="30" />
            </n-form-item>
            <n-form-item label="身份证号" class="form-col">
              <n-input v-model:value="form.id_no" placeholder="18 位身份证号，最后一位可为 X" :maxlength="18" />
            </n-form-item>
          </div>
          <div v-if="isCorp" class="form-row">
            <n-form-item label="企业名称" class="form-col">
              <n-input v-model:value="form.corp_name" placeholder="请输入营业执照上的企业全称" :maxlength="128" />
            </n-form-item>
            <n-form-item label="营业执照号" class="form-col">
              <n-input v-model:value="form.corp_no" placeholder="统一社会信用代码 / 注册号" :maxlength="64" />
            </n-form-item>
          </div>
          <n-alert type="info" :bordered="false" class="form-alert">
            提交后由平台运营审核，请确保姓名、身份证号与证件完全一致，否则会被驳回。
          </n-alert>
          <n-button type="primary" :loading="saving" @click="handleSubmit">提交认证</n-button>
        </n-form>
      </n-card>

      <n-card v-if="status === 'approved'" :bordered="false" class="panel-card" title="后续操作">
        <p class="note-text">
          认证已通过。若需要变更认证主体，请联系平台运营先撤销当前认证，再重新提交。
        </p>
      </n-card>
    </n-spin>
  </div>
</template>

<style scoped>
.status-alert {
  margin-bottom: 16px;
}
.tips-alert {
  margin-bottom: 16px;
}
.status-line {
  line-height: 1.8;
}
.status-tags {
  margin-top: 10px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.detail-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
  margin-bottom: 16px;
}
.panel-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.form-card {
  margin-top: 4px;
}
.form-row {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.form-col {
  min-width: 0;
}
.form-alert {
  margin: 4px 0 16px;
}
.reject-text {
  color: #dc2626;
}
.note-list {
  margin: 0;
  padding-left: 18px;
  color: #64748b;
  font-size: 13px;
  line-height: 1.9;
}
.note-list li {
  margin-bottom: 6px;
}
.note-text {
  margin: 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.8;
}
@media (max-width: 1000px) {
  .detail-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .form-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
