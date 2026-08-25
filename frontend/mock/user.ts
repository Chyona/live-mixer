import { MockMethod } from 'vite-plugin-mock';
import { API_PREFIX } from './_config';

export default [
  {
    url: `${API_PREFIX}/v1/accounts/:id`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => ({
      code: 0,
      message: '',
      data: { id: query.id || '222', username: 'userName', token: 'mock-token' },
    }),
  },
  {
    url: `${API_PREFIX}/v1/auth/login`,
    method: 'post',
    response: ({ body }: { body: { username?: string; password?: string } }) => {
      const username = body?.username?.trim();
      const password = body?.password;

      if (username && password) {
        // 便于本地联调：密码 wrong 专门模拟账号密码错误
        if (password === 'wrong') {
          return {
            code: 400,
            message: '用户名或密码错误',
            data: null,
          };
        }

        return {
          code: 0,
          message: '',
          data: { id: '222', username, token: 'mock-token' },
        };
      }

      return {
        code: 400,
        message: '用户名或密码错误',
        data: null,
      };
    },
  },
] as MockMethod[];
