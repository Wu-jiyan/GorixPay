import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Form, Input, Button, Card, Message } from '@arco-design/web-react';
import api from '@/api/client';

const MerchantLogin: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const onSubmit = async (values: { identifier: string; password: string }) => {
    setLoading(true);
    try {
      await api.post('/auth/merchant/login', values);
      Message.success('登录成功');
      navigate('/merchant', { replace: true });
    } catch {
      Message.error('账号或密码错误');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f5f5f5' }}>
      <Card style={{ width: 400 }} title="商户登录">
        <Form layout="vertical" onSubmit={onSubmit}>
          <Form.Item field="identifier" label="账号" rules={[{ required: true, message: '请输入账号' }]}>
            <Input placeholder="请输入邮箱或手机号" />
          </Form.Item>
          <Form.Item field="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" long loading={loading}>
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default MerchantLogin;
