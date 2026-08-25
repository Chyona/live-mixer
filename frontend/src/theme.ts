import { ThemeConfig } from 'antd';

/**
 * Theme configuration for Ant Design components.
 * https://ant.design/docs/react/customize-theme
 * 语义色与 src/style/root.css 保持一致。
 * 圆角：按钮/输入 8px（radius-md），卡片/弹窗 12px（radius-lg）。
 */
export const theme = {
  token: {
    colorPrimary: '#356bfd',
    colorInfo: '#1677ff',
    colorSuccess: '#52c41a',
    colorWarning: '#faad14',
    colorError: '#ff4d4f',
    fontSize: 14,
    colorBorder: '#e0e7ff',
    borderRadius: 8,
    borderRadiusLG: 12,
  },
  components: {
    Tabs: {},
    Button: {
      borderRadius: 8,
    },
    Drawer: {
      fontSizeLG: 16,
      borderRadiusLG: 12,
    },
    Tooltip: {
      colorBgSpotlight: '#fff',
      colorTextLightSolid: 'rgba(0, 0, 0, 0.85)',
    },
    Modal: {
      borderRadiusLG: 12,
      titleFontSize: 16,
      titleLineHeight: 1.5,
    },
    Form: {
      labelColor: '#8A8C99',
      labelFontSize: 12,
    },
    Table: {},
  },
} as ThemeConfig;

export default theme;
