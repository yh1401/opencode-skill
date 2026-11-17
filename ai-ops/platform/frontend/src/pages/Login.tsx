import { useState } from 'react';
import { App as AntdApp, Button, Card, Form, Input, Typography } from 'antd';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAuth } from '../AuthContext';

const { Title, Text } = Typography;

/** 登录页：本地账号登录；企业统一认证（SSO）入口按配置启用后再开放。 */
export default function Login() {
  const { login } = useAuth();
  const { message } = AntdApp.useApp();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [submitting, setSubmitting] = useState(false);

  const onFinish = async (values: { username: string; password: string }) => {
    setSubmitting(true);
    try {
      await login(values.username, values.password);
      const redirect = params.get('redirect') || '/dashboard';
      navigate(redirect, { replace: true });
    } catch (err) {
      message.error(err instanceof Error ? err.message : '登录失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="login-page">
      <Card className="login-card" bordered={false}>
        <div className="login-brand">
          <span className="brand-mark">视联</span>
          <Title level={4} style={{ margin: 0 }}>
            AI 运维平台
          </Title>
        </div>
        <Text type="secondary">请使用平台账号登录，不同角色可见的菜单与操作不同。</Text>
        <Form layout="vertical" onFinish={onFinish} requiredMark={false} style={{ marginTop: 20 }}>
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} autoComplete="username" autoFocus />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={submitting}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  );
}
