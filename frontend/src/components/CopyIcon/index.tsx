import type { CopyConfig } from 'antd/es/typography/Base';
import { LuCheck, LuCopy } from 'react-icons/lu';

export const COPY_ICON_SIZE = 14;

export function CopyIcon({ size = COPY_ICON_SIZE, className }: { size?: number; className?: string }) {
  return (
    <span className={['app-copy-icon', className].filter(Boolean).join(' ')} aria-hidden>
      <LuCopy size={size} />
    </span>
  );
}

export function CopiedIcon({ size = COPY_ICON_SIZE, className }: { size?: number; className?: string }) {
  return (
    <span className={['app-copy-icon', 'app-copy-icon_copied', className].filter(Boolean).join(' ')} aria-hidden>
      <LuCheck size={size} />
    </span>
  );
}

/** Ant Design Typography copyable 统一图标配置 */
export function buildCopyableConfig(text: string, overrides?: Partial<CopyConfig>): CopyConfig {
  return {
    text,
    icon: [<CopyIcon key="copy" />, <CopiedIcon key="copied" />],
    ...overrides,
  };
}
