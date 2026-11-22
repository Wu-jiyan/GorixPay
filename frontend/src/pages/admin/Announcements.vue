<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NSwitch, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { AddOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { Announcement } from '@/api';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Announcement[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);
const pendingStatusId = ref('');
const removingId = ref('');

const form = reactive({
  title: '',
  content: '',
  status: 'draft',
});

const rules: FormRules = {
  title: { required: true, message: '请输入公告标题', trigger: ['blur', 'input'] },
  content: { required: true, message: '请输入公告内容', trigger: ['blur', 'input'] },
};

const isEditing = computed(() => editingId.value !== null);

const publishSwitch = computed({
  get: () => form.status === 'published',
  set: (value: boolean) => {
    form.status = value ? 'published' : 'draft';
  },
});

async function load() {
  loading.value = true;
  try {
    rows.value = await adminApi.announcements();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingId.value = null;
  form.title = '';
  form.content = '';
  form.status = 'draft';
  showForm.value = true;
}

function openEdit(row: Announcement) {
  editingId.value = row.id;
  form.title = row.title ?? '';
  form.content = row.content ?? '';
  form.status = row.status ?? 'draft';
  showForm.value = true;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload = { title: form.title.trim(), content: form.content, status: form.status };
    if (editingId.value) {
      await adminApi.updateAnnouncement(editingId.value, payload);
      message.success('公告已更新');
    } else {
      await adminApi.createAnnouncement(payload);
      message.success('公告已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function togglePublished(row: Announcement) {
  const next = row.status === 'published' ? 'draft' : 'published';
  pendingStatusId.value = row.id;
  try {
    await adminApi.updateAnnouncement(row.id, {
      title: row.title,
      content: row.content,
      status: next,
    });
    message.success(next === 'published' ? '公告已发布' : '公告已转为草稿');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    pendingStatusId.value = '';
  }
}

async function remove(row: Announcement) {
  removingId.value = row.id;
  try {
    await adminApi.deleteAnnouncement(row.id);
    message.success('公告已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingId.value = '';
  }
}

function statusTag(status?: string) {
  const published = status === 'published';
  return h(
    NTag,
    { size: 'small', round: true, type: published ? 'success' : 'default', bordered: false },
    { default: () => (published ? '已发布' : '草稿') },
  );
}

const columns = computed<DataTableColumns<Announcement>>(() => [
  { title: '标题', key: 'title', minWidth: 220, ellipsis: true, render: (row) => row.title || '-' },
  { title: '状态', key: 'status', width: 110, render: (row) => statusTag(row.status) },
  {
    title: '发布时间',
    key: 'published_at',
    width: 170,
    render: (row) => formatDateTime(row.published_at),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 190,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 12, wrap: false },
        {
          default: () => [
            h(
              NButton,
              { text: true, size: 'small', type: 'primary', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NPopconfirm,
              {
                positiveText: '确定',
                negativeText: '取消',
                onPositiveClick: () => togglePublished(row),
              },
              {
                trigger: () =>
                  h(
                    NSwitch,
                    {
                      value: row.status === 'published',
                      size: 'small',
                      loading: pendingStatusId.value === row.id,
                    },
                    { checked: () => '发布', unchecked: () => '草稿' },
                  ),
                default: () =>
                  row.status === 'published' ? '确认把该公告转为草稿（下线）？' : '确认发布该公告？',
              },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => remove(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'error',
                      loading: removingId.value === row.id,
                    },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该公告？删除后不可恢复。',
              },
            ),
          ],
        },
      ),
  },
]);

onMounted(load);
</script>

<template>
  <admin-page title="公告管理" subtitle="站点公告的发布、编辑与下线">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" @click="openCreate">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新建公告
        </n-button>
      </n-space>
    </template>

    <n-card :bordered="false">
      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: Announcement) => row.id"
        :scroll-x="1000"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="暂无公告，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑公告' : '新建公告'"
      style="width: 720px; max-width: 94vw"
    >
      <n-form
        ref="formRef"
        :model="form"
        :rules="rules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
        <n-form-item label="标题" path="title">
          <n-input v-model:value="form.title" placeholder="例如：系统升级维护通知" />
        </n-form-item>
        <n-form-item label="内容" path="content">
          <n-input
            v-model:value="form.content"
            type="textarea"
            :autosize="{ minRows: 6, maxRows: 14 }"
            placeholder="支持纯文本，换行会保留"
          />
        </n-form-item>
        <n-form-item label="状态">
          <n-switch v-model:value="publishSwitch">
            <template #checked>已发布</template>
            <template #unchecked>草稿</template>
          </n-switch>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
