import React, { useCallback, useEffect, useState } from 'react';
import { Card, Form, Input, Button, Message, Spin } from '@arco-design/web-react';
import { adminApi, SystemSetting } from '@/api/admin';

const Settings: React.FC = () => {
  const [settings, setSettings] = useState<SystemSetting[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState<string | null>(null);

  useEffect(() => {
    adminApi.settings().then((res) => { const items = res.data.items; setSettings(items); }).finally(() => setLoading(false));
  }, []);

  const save = useCallback(async (key: string, value: string) => {
    setSaving(key);
    try {
      await adminApi.updateSetting(key, value);
      Message.success('已保存');
    } catch {
      Message.error('保存失败');
    } finally {
      setSaving(null);
    }
  }, []);

  if (loading) return <Spin loading style={{ display: 'flex', justifyContent: 'center', marginTop: 100 }} />;

  return (
    <Card title="系统设置">
      <Form layout="vertical" style={{ maxWidth: 600 }}>
        {settings.map((s) => (
          <SettingItem key={s.key} setting={s} saving={saving === s.key} onSave={save} />
        ))}
      </Form>
    </Card>
  );
};

const SettingItem: React.FC<{
  setting: SystemSetting;
  saving: boolean;
  onSave: (key: string, value: string) => void;
}> = ({ setting, saving, onSave }) => {
  const [value, setValue] = useState(setting.value);

  return (
    <Form.Item label={setting.key}>
      <Input.Group compact style={{ display: 'flex' }}>
        <Input value={value} onChange={setValue} style={{ flex: 1 }} />
        <Button type="primary" loading={saving} onClick={() => onSave(setting.key, value)}>
          保存
        </Button>
      </Input.Group>
    </Form.Item>
  );
};

export default Settings;
