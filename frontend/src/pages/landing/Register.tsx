import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Form, Input, Button, Card, Message } from '@arco-design/web-react';
import axios from 'axios';

const Register: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const [form] = Form.useForm();

  const onFinish = async (values: { identifier: string; password: string; confirmPassword: string }) => {
    if (values.password !== values.confirmPassword) {
      Message.error('两次输入的密码不一致');
      return;
    }
    setLoading(true);
    try {
      await axios.post('/api/v1/auth/register', {
        identifier: values.identifier,
        password: values.password,
      });
      Message.success('注册成功');
      navigate('/merchant', { replace: true });
    } catch (err: unknown) {
      if (axios.isAxiosError(err) && err.response?.status === 409) {
        Message.error('该账号已被注册');
      } else {
        Message.error('注册失败，请稍后重试');
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f5f5f5' }}>
      <Card style={{ width: 400 }} title="注册商户账号">
        <Form form={form} layout="vertical" onSubmit={onFinish}>
          <Form.Item field="identifier" label="账号" rules={[{ required: true, message: '请输入账号' }]}>
            <Input placeholder="请输入账号" />
          </Form.Item>
          <Form.Item field="password" label="密码" rules={[{ required: true, minLength: 4, message: '密码至少4位' }]}>
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
          <Form.Item
            field="confirmPassword"
            label="确认密码"
            rules={[
              { required: true, message: '请确认密码' },
              {
                validator: (value, callback) => {
                  if (value !== form.getFieldValue('password')) {
                    callback('两次输入的密码不一致');
                  } else {
                    callback();
                  }
                },
              },
            ]}
          >
            <Input.Password placeholder="请再次输入密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" long loading={loading}>
              注册
            </Button>
          </Form.Item>
          <div style={{ textAlign: 'center' }}>
            <span>已有账号？</span>
            <Link to="/merchant/login">立即登录</Link>
          </div>
        </Form>
      </Card>
    </div>
  );
};

export default Register;
