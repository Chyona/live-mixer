/**
 * 与 root.css 语义色 token 保持一致，供 TS 内联样式使用。
 * 修改颜色时请同步更新 src/style/root.css。
 */
export const semanticColors = {
  primary: '#356bfd',
  info: '#1677ff',
  success: '#52c41a',
  warning: '#fa8c16',
  danger: '#ff4d4f',
  purple: '#722ed1',
  pink: '#eb2f96',
  cyan: '#13c2c2',
} as const;

export const speakerColors = [
  semanticColors.info,
  semanticColors.success,
  semanticColors.warning,
  semanticColors.pink,
  semanticColors.purple,
  semanticColors.cyan,
] as const;
