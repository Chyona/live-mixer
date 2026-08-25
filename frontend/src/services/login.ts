import { type BaseResponse, request } from './http';

export type UserLoginParams = { username: string; password: string };

export enum UserRole {
  Normal = 'normal',
  Admin = 'admin',
}

export interface UserLoginResult {
  id: string;
  username: string;
  role?: UserRole;
  token?: string;
}

export async function pwdlogin(params: UserLoginParams): Promise<BaseResponse<UserLoginResult>> {
  return await request('/v1/auth/login', {
    method: 'post',
    data: params,
  });
}

/** 本地会话是否具备进入受保护路由的最低凭证（id + token） */
export function hasAuthCredentials(info: Partial<UserLoginResult> | null | undefined): boolean {
  return Boolean(info?.id && typeof info.token === 'string' && info.token.length > 0);
}
