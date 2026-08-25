import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { Button, Form, Input } from 'antd';
import { useState } from 'react';
import { useAuth } from '~/context/AuthContext';
import { isBusinessSuccess } from '~/services/businessCodes';
import { pwdlogin } from '~/services/login';
import { AppError } from '~/services/http';
import { toast } from '~/utils/toast';
import { trackEvent } from '~/utils/gtm';

type PasswordFormValues = {
  username: string;
  password: string;
};

const PasswordLogin = () => {
  const { updateAuthInfo } = useAuth();
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (values: PasswordFormValues) => {
    setLoading(true);
    try {
      const { code, data, message } = await pwdlogin({
        username: values.username.trim(),
        password: values.password,
      });

      // code === 0 成功；其他 code 一律按异常处理
      if (!isBusinessSuccess(code)) {
        toast.notify.error(message?.trim() || '登录失败');
        return;
      }

      if (!data) {
        toast.notify.error('登录失败，未返回用户信息');
        return;
      }

      trackEvent('passwordLogin', {
        userId: data.id || '*',
      });
      updateAuthInfo(data);
    } catch (error) {
      // 登录场景不走 showAppError：HTTP 401 会被当成会话过期而静默吞掉
      if (error instanceof AppError) {
        toast.notify.error(error.errorMessage?.trim() || '登录失败');
      } else {
        toast.notify.error('登录失败，请稍后重试');
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <Form
      className="password-login"
      layout="vertical"
      size="large"
      onFinish={handleSubmit}
      autoComplete="off"
    >
      <Form.Item
        name="username"
        label="用户名"
        rules={[{ required: true, message: '请输入用户名' }]}
      >
        <Input prefix={<UserOutlined />} placeholder="请输入用户名" allowClear />
      </Form.Item>
      <Form.Item
        name="password"
        label="密码"
        rules={[{ required: true, message: '请输入密码' }]}
      >
        <Input.Password prefix={<LockOutlined />} placeholder="请输入密码" />
      </Form.Item>
      <Form.Item className="password-login-submit">
        <Button type="primary" htmlType="submit" block loading={loading}>
          登录
        </Button>
      </Form.Item>
    </Form>
  );
};

export default PasswordLogin;
