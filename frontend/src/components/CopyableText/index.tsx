import { Typography } from 'antd';
import type { ReactNode } from 'react';

import EllipsisTooltip from '~/components/EllipsisTooltip';
import { CopyIcon, buildCopyableConfig } from '~/components/CopyIcon';
import { copyTextToClipboard } from '~/utils/clipboard';
import { toast } from '~/utils/toast';

type CopyableTextLayout = 'row' | 'paragraph';

interface CopyableTextProps {
  text: string;
  className?: string;
  textClassName?: string;
  layout?: CopyableTextLayout;
  successMessage?: string;
  emptyFallback?: ReactNode;
}

async function copyWithToast(text: string, successMessage = '已复制') {
  const copied = await copyTextToClipboard(text);
  if (copied) {
    toast.notify.success(successMessage);
    return true;
  }
  toast.notify.error('复制失败，请手动复制');
  return false;
}

/** 点击文字或复制图标均可复制 */
export function CopyableText({
  text,
  className,
  textClassName,
  layout = 'row',
  successMessage = '已复制',
  emptyFallback = null,
}: CopyableTextProps) {
  const value = text.trim();
  if (!value) return emptyFallback;

  const runCopy = () => copyWithToast(value, successMessage);

  if (layout === 'paragraph') {
    return (
      <Typography.Paragraph
        className={['app-copyable-text', 'app-copyable-text_paragraph', className]
          .filter(Boolean)
          .join(' ')}
        copyable={buildCopyableConfig(value, {
          onCopy: () => {
            toast.notify.success(successMessage);
          },
        })}
        onClick={(event) => {
          const target = event.target as HTMLElement;
          if (target.closest('.ant-typography-copy')) return;
          void runCopy();
        }}
      >
        {value}
      </Typography.Paragraph>
    );
  }

  return (
    <div className={['app-copyable-text', 'app-copyable-text_row', className].filter(Boolean).join(' ')}>
      <button
        type="button"
        className={['app-copyable-text__trigger', textClassName].filter(Boolean).join(' ')}
        title="点击复制"
        onClick={() => void runCopy()}
      >
        <EllipsisTooltip text={value} className="list-page__cell-ellipsis app-copyable-text__label" />
      </button>
      <button
        type="button"
        className="app-copyable-text__icon-btn"
        aria-label="复制"
        title="复制"
        onClick={() => void runCopy()}
      >
        <CopyIcon />
      </button>
    </div>
  );
}

export default CopyableText;
