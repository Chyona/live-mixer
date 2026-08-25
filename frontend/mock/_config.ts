import { loadEnv } from 'vite';

const mode = process.env.MODE || process.env.NODE_ENV || 'development';
const env = loadEnv(mode, process.cwd(), '');

/** 从 .env 读取，与前端 apiPath 使用同一前缀 */
export const API_PREFIX = (env.VITE_API_PREFIX || '/openapi').replace(/\/$/, '');
