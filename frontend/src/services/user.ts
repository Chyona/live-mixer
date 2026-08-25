import { UserLoginResult } from './login';
import { BaseResponse, request } from './http';

export async function getUserInfo(id: string): Promise<BaseResponse<UserLoginResult>> {
  return await request(`/v1/accounts/${id}`, {
    method: 'get'
  });
}
