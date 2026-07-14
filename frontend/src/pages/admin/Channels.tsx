import React, { useCallback, useEffect, useState } from 'react';
import {
  Card, Button, Message, Modal, Form, Input, InputNumber, Select, Space, Switch,
} from '@arco-design/web-react';
import { IconPlus } from '@arco-design/web-react/icon';
import { adminApi, Channel, ChannelRequest } from '@/api/admin';
import StatusTag from '@/components/StatusTag';

const Channels: React.FC = () => {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.channels();
      setChannels(res.data.items);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetch(); }, [fetch]);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    setModalVisible(true);
  };

  const openEdit = (ch: Channel) => {
    setEditing(ch);
    form.setFieldsValue({ name: ch.name, plugin_code: ch.plugin_code, payment_type: ch.payment_type, priority: ch.priority, weight: ch.weight, api_url: '', pid: '', key: '' });
    setModalVisible(true);
  };

  const onSubmit = async () => {
    try {
      const values = await form.validate();
      setSubmitting(true);
      const req: ChannelRequest = { ...values, status: 'active' };
      if (editing) {
        await adminApi.updateChannel(editing.id, req);
        Message.success('已更新');
      } else {
        await adminApi.createChannel(req);
        Message.success('已创建');
      }
      setModalVisible(false);
      fetch();
    } catch {
      // validation error
    } finally {
      setSubmitting(false);
    }
  };

  const toggleStatus = async (id: string, current: string) => {
    const newStatus = current === 'active' ? 'disabled' : 'active';
    try {
      await adminApi.updateChannelStatus(id, newStatus);
      Message.success('状态已更新');
      fetch();
    } catch {
      Message.error('操作失败');
    }
  };

  return (
    <Card
      title="渠道管理"
      extra={<Button type="primary" icon={<IconPlus />} onClick={openCreate}>添加渠道</Button>}
    >
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ borderBottom: '1px solid #e5e6eb', textAlign: 'left' }}>
            <th style={{ padding: 8 }}>名称</th>
            <th style={{ padding: 8 }}>插件</th>
            <th style={{ padding: 8 }}>支付方式</th>
            <th style={{ padding: 8 }}>优先级</th>
            <th style={{ padding: 8 }}>权重</th>
            <th style={{ padding: 8 }}>状态</th>
            <th style={{ padding: 8 }}>已配置</th>
            <th style={{ padding: 8 }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr><td colSpan={8} style={{ padding: 16, textAlign: 'center' }}>加载中...</td></tr>
          ) : channels.length === 0 ? (
            <tr><td colSpan={8} style={{ padding: 16, textAlign: 'center' }}>暂无渠道</td></tr>
          ) : (
            channels.map((ch) => (
              <tr key={ch.id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                <td style={{ padding: 8 }}>{ch.name}</td>
                <td style={{ padding: 8 }}><code>{ch.plugin_code}</code></td>
                <td style={{ padding: 8 }}>{ch.payment_type}</td>
                <td style={{ padding: 8 }}>{ch.priority}</td>
                <td style={{ padding: 8 }}>{ch.weight}</td>
                <td style={{ padding: 8 }}><StatusTag status={ch.status} /></td>
                <td style={{ padding: 8 }}>{ch.configured ? '是' : '否'}</td>
                <td style={{ padding: 8 }}>
                  <Space>
                    <Button size="small" type="text" onClick={() => openEdit(ch)}>编辑</Button>
                    <Button
                      size="small"
                      type="text"
                      status={ch.status === 'active' ? 'danger' : 'success'}
                      onClick={() => toggleStatus(ch.id, ch.status)}
                    >
                      {ch.status === 'active' ? '禁用' : '启用'}
                    </Button>
                  </Space>
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>

      <Modal
        title={editing ? '编辑渠道' : '添加渠道'}
        visible={modalVisible}
        onOk={onSubmit}
        onCancel={() => setModalVisible(false)}
        confirmLoading={submitting}
      >
        <Form form={form} layout="vertical">
          <Form.Item field="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item field="plugin_code" label="插件代码" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item field="payment_type" label="支付方式" rules={[{ required: true }]}><Input placeholder="alipay / wxpay 等" /></Form.Item>
          <Form.Item field="priority" label="优先级" rules={[{ required: true }]}><InputNumber min={0} /></Form.Item>
          <Form.Item field="weight" label="权重" rules={[{ required: true }]}><InputNumber min={1} /></Form.Item>
          <Form.Item field="api_url" label="API 地址" rules={editing ? [] : [{ required: true }]}>
            <Input placeholder={editing ? '留空则不修改' : 'https://example.com/'} />
          </Form.Item>
          <Form.Item field="pid" label="PID" rules={editing ? [] : [{ required: true }]}>
            <Input placeholder={editing ? '留空则不修改' : ''} />
          </Form.Item>
          <Form.Item field="key" label="密钥" rules={editing ? [] : [{ required: true }]}>
            <Input.Password placeholder={editing ? '留空则不修改' : ''} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
};

export default Channels;
