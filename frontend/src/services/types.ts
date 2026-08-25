export interface BaseResponse<T = unknown> {
  code: number;
  message: string;
  data: T;
}
