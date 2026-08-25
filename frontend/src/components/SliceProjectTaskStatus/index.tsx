import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Alert } from 'antd';

import { buildTasksListLink } from '~/routes/links';

import './SliceProjectTaskStatus.css';

interface SliceProjectTaskReadOnlyAlertProps {
  /** 项目名称，用于跳转任务列表搜索 */
  projectName?: string;
  /** 进行中 + 待进行任务数 */
  runningTaskCount: number;
  /** 是否展示（无项目 id 时为 false） */
  visible?: boolean;
}

/** 有进行中任务时的只读提示条 */
export function SliceProjectTaskReadOnlyAlert({
  projectName,
  runningTaskCount,
  visible = true,
}: SliceProjectTaskReadOnlyAlertProps) {
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    // 任务全部结束后，下次再有进行中任务时重新显示
    if (runningTaskCount <= 0) {
      setDismissed(false);
    }
  }, [runningTaskCount]);

  if (!visible || runningTaskCount <= 0 || dismissed) return null;

  const tasksLink = buildTasksListLink({ keyword: projectName?.trim() || null });

  return (
    <Alert
      className="slice-project-task-alert"
      type="warning"
      showIcon
      closable
      onClose={() => setDismissed(true)}
      message={
        <span>
          当前有 {runningTaskCount} 个任务正在处理，请等待完成后再编辑。
          <Link to={tasksLink} className="slice-project-task-alert__link">
            查看任务
          </Link>
        </span>
      }
    />
  );
}
