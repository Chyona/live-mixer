/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_APP_TITLE?: string;
  readonly VITE_APP_DESCRIPTION?: string;
  readonly VITE_SITE_URL?: string;
  readonly VITE_COMPANY_NAME?: string;
  readonly VITE_NAV_LAYOUT?: 'top' | 'left';
  readonly VITE_API_PREFIX?: string;
  readonly VITE_API_PROXY_TARGET?: string;
  readonly VITE_LOGIN_MODE?: 'page' | 'modal';
  readonly VITE_GTM_ID?: string;
  readonly VITE_ENABLE_GTM?: string;
  readonly VITE_ENABLE_FLOAT?: string;
  readonly VITE_CONTACT_QRCODE_URL?: string;
  readonly VITE_SUPPORT_TITLE?: string;
  /** 剪映小助手中转页外链 */
  readonly VITE_JIANYING_ASSISTANT_URL?: string;
  readonly VITE_MOCK?: string;
  readonly VITE_APP_ENV?: 'test' | 'dev' | 'pre' | 'prod';
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
