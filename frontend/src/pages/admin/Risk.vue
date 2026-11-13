<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { BlacklistEntry, RiskEvent } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime } from '@/utils/format';
import { httpStatus, itemsOf, numberOr, useServerPaging } from './shared';

const message = useMessage();

const tab = ref('blacklist');

const entries = ref<BlacklistEntry[]>([]);
const entriesLoading = ref(false);
const entriesUnavailable = ref(false);
const entryTypeFilter = ref<string | null>(null);

const events = ref<RiskEvent[]>([]);
const eventsLoading = ref(false);
const eventsUnavailable = ref(false);
const merchantId = ref('');
const ruleCode = ref('');

const showCreate = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const form = reactive({ entry_type: 'ip', value: '', remark: '' });

const {
  page: eventsPage,
  pageSize: eventsPageSize,
  total: eventsTotal,
  pagination: eventsPagination,
  onPage: onEventsPage,
  onPageSize: onEventsPageSize,
  resetPage: resetEventsPage,
} = useServerPaging(loadEvents);

const entryTypeOptions: SelectOption[] = [
  { label: 'IP', value: 'ip' },
  { label: '域名', value: 'domain' },
  { label: '关键词', value: 'keyword' },
  { label: '邮箱', value: 'email' },
];

const entryTypeLabels: Record<string, string> = {
  ip: 'IP',
  domain: '域名',
  keyword: '关键词',
  email: '邮箱',
};

const rules: FormRules = {
  entry_type: { required: true, message: '请选择类型', trigger: ['change', 'blur'] },
  value: { required: true, message: '请输入拦截值', trigger: ['blur', 'input'] },
};

async function loadEntries() {
  entriesLoading.value = true;
  try {
    entries.value = itemsOf<BlacklistEntry>(
      await adminApi.blacklist({
        entry_type: entryTypeFilter.value ?? undefined,
      }),
    );
    entriesUnavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      entries.value = [];
      entriesUnavailable.value = true;
      return;
    }
    message.error(errorMessage(error));
  } finally {
    entriesLoading.value = false;
  }
}

async function loadEvents() {
  eventsLoading.value = true;
  try {
    const data = await adminApi.riskEvents({
      page: eventsPage.value,
      page_size: eventsPageSize.value,
      merchant_id: merchantId.value.trim() || undefined,
      rule_code: ruleCode.value.trim() || undefined,
    });
    const items = itemsOf<RiskEvent>(data.items);
    events.value = items;
    eventsTotal.value = numberOr(data.total, items.length);
    eventsUnavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      events.value = [];
      eventsTotal.value = 0;
      eventsUnavailable.value = true;
      return;
    }
    message.error(errorMessage(error));
  } finally {
    eventsLoading.value = false;
  }
}

function handleTab(value: string) {
  tab.value = value;
  if (value === 'blacklist') void loadEntries();
  else void loadEvents();
}

function openCreate() {
  form.entry_type = 'ip';
  form.value = '';
  form.remark = '';
  showCreate.value = true;
}

async function submitCreate() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    await adminApi.createBlacklist({
      entry_type: form.entry_type,
      value: form.value.trim(),
      remark: form.remark.trim() || undefined,
    });
    message.success('已加入黑名单');
    showCreate.value = false;
    await loadEntries();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function removeEntry(row: BlacklistEntry) {
  try {
    await adminApi.deleteBlacklist(row.id);
    message.success('已移出黑名单');
    await loadEntries();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function searchEvents() {
  resetEventsPage();
  void loadEvents();
}

function resetEventFilters() {
  merchantId.value = '';
  ruleCode.value = '';
  resetEventsPage();
  void loadEvents();
}

const entryColumns = computed<DataTableColumns<BlacklistEntry>>(() => [
  {
    title: '类型',
    key: 'entry_type',
    width: 100,
    render: (row) =>
      h(NTag, { size: 'small', round: true, type: 'error' }, {
        default: () => entryTypeLabels[row.entry_type] ?? row.entry_type ?? '-',
      }),
  },
  {
    title: '拦截值',
    key: 'value',
    minWidth: 240,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.value || '-'),
  },
  { title: '备注', key: 'remark', minWidth: 180, ellipsis: true, render: (row) => row.remark || '-' },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    fixed: 'right',
    render: (row) =>
      h(
        NPopconfirm,
        { onPositiveClick: () => removeEntry(row) },
        {
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
          default: () => '确认将该条目移出黑名单？',
        },
      ),
  },
]);

const eventColumns = computed<DataTableColumns<RiskEvent>>(() => [
  {
    title: '规则',
    key: 'rule_code',
    width: 180,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.rule_code || '-'),
  },
  {
    title: '详情',
    key: 'detail',
    minWidth: 280,
    ellipsis: true,
    render: (row) => h('span', { title: row.detail || '-' }, row.detail || '-'),
  },
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '客户端 IP',
    key: 'client_ip',
    width: 140,
    render: (row) => h('span', { class: 'mono' }, row.client_ip || '-'),
  },
  {
    title: '发生时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
]);

onMounted(() => {
  void loadEntries();
});
</script>

<template>
  <div class="page">
    <PageHeader title="风控与黑名单" subtitle="IP / 域名 / 关键词拦截名单与风控命中事件">
      <template #actions>
        <n-button
          secondary
          :loading="entriesLoading || eventsLoading"
          @click="handleTab(tab)"
        >
          刷新当前页签
        </n-button>
        <n-button v-if="tab === 'blacklist'" type="primary" @click="openCreate">添加黑名单</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="blacklist" tab="黑名单">
          <div class="toolbar">
            <n-select
              v-model:value="entryTypeFilter"
              :options="entryTypeOptions"
              placeholder="全部类型"
              clearable
              style="width: 150px"
              @update:value="loadEntries"
            />
            <n-button @click="loadEntries">刷新</n-button>
          </div>
          <n-empty v-if="entriesUnavailable" description="接口未启用" size="large">
            <template #extra>
              <n-button size="small" :loading="entriesLoading" @click="loadEntries">重新检测</n-button>
            </template>
          </n-empty>
          <n-data-table
            v-else
            :columns="entryColumns"
            :data="entries"
            :loading="entriesLoading"
            :pagination="{ pageSize: 10 }"
            :row-key="(row: BlacklistEntry) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
          >
            <template #empty>
              <n-empty description="暂无黑名单条目" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="events" tab="风控事件">
          <div class="toolbar">
            <n-input
              v-model:value="merchantId"
              placeholder="商户 ID"
              clearable
              style="width: 250px"
              @keyup.enter="searchEvents"
            />
            <n-input
              v-model:value="ruleCode"
              placeholder="规则编码，例如 risk.ip_limit"
              clearable
              style="width: 240px"
              @keyup.enter="searchEvents"
            />
            <n-button type="primary" @click="searchEvents">查询</n-button>
            <n-button @click="resetEventFilters">重置</n-button>
            <span class="muted" style="font-size: 12px">共 {{ eventsTotal }} 条</span>
          </div>
          <n-empty v-if="eventsUnavailable" description="接口未启用" size="large">
            <template #extra>
              <n-button size="small" :loading="eventsLoading" @click="loadEvents">重新检测</n-button>
            </template>
          </n-empty>
          <n-data-table
            v-else
            :columns="eventColumns"
            :data="events"
            :loading="eventsLoading"
            :pagination="eventsPagination"
            :row-key="(row: RiskEvent) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onEventsPage"
            @update:page-size="onEventsPageSize"
          >
            <template #empty>
              <n-empty description="暂无风控事件" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal v-model:show="showCreate" preset="card" title="添加黑名单" style="width: 520px; max-width: 94vw">
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="80">
        <n-form-item label="类型" path="entry_type">
          <n-select v-model:value="form.entry_type" :options="entryTypeOptions" />
        </n-form-item>
        <n-form-item label="拦截值" path="value">
          <n-input
            v-model:value="form.value"
            :placeholder="form.entry_type === 'ip' ? '例如 1.2.3.4' : '例如 example.com / 敏感词'"
          />
        </n-form-item>
        <n-form-item label="备注">
          <n-input v-model:value="form.remark" placeholder="可选，记录来源或原因" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submitCreate">确认添加</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>
