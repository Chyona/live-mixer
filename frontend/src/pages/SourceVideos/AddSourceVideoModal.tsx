import { Button, DatePicker, Form, Input, Modal, Radio } from 'antd';
import type { Dayjs } from 'dayjs';
import { useState } from 'react';

import { AppError } from '~/services/http';
import type { BaseResponse } from '~/services/types';
import {
  createSourceVideo,
  isSourceVideoUrlDuplicateError,
  normalizeSourceVideo,
  type SourceMode,
  type SourceVideo,
} from '~/services/sourceVideo';
import { showAppError, toast } from '~/utils/toast';

type FormValues = {
  name: string;
  sourceMode: SourceMode;
  liveUrl: string;
  scheduledAt?: Dayjs;
  remark?: string;
};

interface AddSourceVideoModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** URL 已存在时，点击 toast「查看」：用接口返回的源视频名称搜索 */
  onViewExisting?: (name: string) => void;
}

function readDuplicateSourceVideo(data: unknown): SourceVideo | null {
  if (!data || typeof data !== 'object') return null;
  const video = normalizeSourceVideo(data as Partial<SourceVideo> & Record<string, unknown>);
  return video.name.trim() || video.live_url.trim() || video.m3u8_url.trim() ? video : null;
}

function isHttpUrl(value: string): boolean {
  return /^https?:\/\/.+/i.test(value);
}

function isM3u8Url(value: string): boolean {
  return /\.m3u8(\?|$)/i.test(value);
}

const AddSourceVideoModal = ({
  open,
  onClose,
  onSuccess,
  onViewExisting,
}: AddSourceVideoModalProps) => {
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);
  const sourceMode = Form.useWatch('sourceMode', form) || 'replay';

  const handleClose = () => {
    form.resetFields();
    onClose();
  };

  const showUrlDuplicateToast = (video: SourceVideo | null, message?: string) => {
    const name = video?.name?.trim() || '';
    const key = `source-video-url-exists:${video?.id || video?.m3u8_url || video?.live_url || name || 'unknown'}`;
    toast.notify.warning(message || '直播地址已存在', '可点击查看', {
      key,
      duration: 8,
      btn: name ? (
        <Button
          type="primary"
          size="small"
          onClick={() => {
            toast.notify.destroy(key);
            onViewExisting?.(name);
          }}
        >
          查看
        </Button>
      ) : undefined,
    });
  };

  const handleSubmit = async (values: FormValues) => {
    setSubmitting(true);
    const url = values.liveUrl.trim();
    const mode = values.sourceMode;

    try {
      const response = await createSourceVideo({
        name: values.name.trim(),
        source_mode: mode,
        remark: values.remark?.trim(),
        scheduled_at:
          mode === 'upcoming' && values.scheduledAt
            ? values.scheduledAt.toISOString()
            : undefined,
        ...(isM3u8Url(url) ? { m3u8_url: url } : { live_url: url }),
      });

      if (response.code !== 0) {
        if (isSourceVideoUrlDuplicateError(response)) {
          showUrlDuplicateToast(readDuplicateSourceVideo(response.data), response.message);
          return;
        }
        toast.notify.error(response.message || '添加失败');
        return;
      }

      if (mode === 'upcoming') {
        toast.notify.success('源视频已添加，将按计划探测开播');
      } else if (mode === 'live') {
        toast.notify.success('源视频已添加，正在连接直播');
      } else {
        toast.notify.success('源视频已添加，正在进行 ASR 转写');
      }
      handleClose();
      onSuccess();
    } catch (error) {
      if (error instanceof AppError) {
        if (isSourceVideoUrlDuplicateError({ code: error.errorCode })) {
          const payload = error.resp?.response?.data as BaseResponse<unknown> | undefined;
          showUrlDuplicateToast(readDuplicateSourceVideo(payload?.data), payload?.message);
          return;
        }
        showAppError(error);
      } else {
        toast.notify.error('添加失败');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const urlLabel =
    sourceMode === 'replay' ? '回放地址（m3u8 或 mp4）' : 'm3u8 拉流地址';

  return (
    <Modal
      className="noanimation-modal"
      title="添加源视频"
      open={open}
      okText="添加"
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnClose
      onCancel={handleClose}
      onOk={() => form.submit()}
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        initialValues={{ sourceMode: 'replay' }}
      >
        <Form.Item
          name="name"
          label="直播名称"
          rules={[{ required: true, whitespace: true, message: '请输入直播名称' }]}
        >
          <Input placeholder="请输入直播源名称" maxLength={64} allowClear />
        </Form.Item>

        <Form.Item name="sourceMode" label="源类型" rules={[{ required: true, message: '请选择源类型' }]}>
          <Radio.Group>
            <Radio.Button value="upcoming">将要直播</Radio.Button>
            <Radio.Button value="live">正在直播</Radio.Button>
            <Radio.Button value="replay">回放</Radio.Button>
          </Radio.Group>
        </Form.Item>

        {sourceMode === 'upcoming' ? (
          <Form.Item
            name="scheduledAt"
            label="计划开播时间"
            extra="开播后 2 小时内未出流将标记失败；可提前最多 15 分钟开始探测"
            rules={[{ required: true, message: '请选择计划开播时间' }]}
          >
            <DatePicker showTime style={{ width: '100%' }} placeholder="选择计划开播时间" />
          </Form.Item>
        ) : null}

        <Form.Item
          name="liveUrl"
          label={urlLabel}
          extra={
            sourceMode === 'live'
              ? '添加后立即连接；2 小时内未出流将标记失败'
              : sourceMode === 'replay'
                ? '回放 m3u8 不会整段转封装上传，ASR 与裁剪直接读取播放列表'
                : undefined
          }
          rules={[
            { required: true, whitespace: true, message: '请输入地址' },
            {
              validator: (_, value: string | undefined) => {
                const trimmed = value?.trim();
                if (!trimmed) return Promise.resolve();
                if (!isHttpUrl(trimmed)) {
                  return Promise.reject(new Error('请输入有效的 http/https 地址'));
                }
                if (sourceMode !== 'replay' && !isM3u8Url(trimmed)) {
                  return Promise.reject(new Error('将要直播 / 正在直播必须填写 m3u8 地址'));
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input
            placeholder={
              sourceMode === 'replay'
                ? 'https://example.com/vod.m3u8 或 https://example.com/replay.mp4'
                : 'https://example.com/live/index.m3u8'
            }
            maxLength={1024}
            allowClear
          />
        </Form.Item>

        <Form.Item name="remark" label="备注">
          <Input placeholder="选填，便于后续搜索识别" maxLength={256} allowClear />
        </Form.Item>
      </Form>
    </Modal>
  );
};

export default AddSourceVideoModal;
