import { Button, Form, Input, Modal } from 'antd';
import { useState } from 'react';

import { AppError } from '~/services/http';
import type { BaseResponse } from '~/services/types';
import {
  createSourceVideo,
  isSourceVideoUrlDuplicateError,
  normalizeSourceVideo,
  type SourceVideo,
} from '~/services/sourceVideo';
import { showAppError, toast } from '~/utils/toast';

type FormValues = {
  name: string;
  liveUrl: string;
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
  return video.name.trim() || video.live_url.trim() ? video : null;
}

const AddSourceVideoModal = ({
  open,
  onClose,
  onSuccess,
  onViewExisting,
}: AddSourceVideoModalProps) => {
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);

  const handleClose = () => {
    form.resetFields();
    onClose();
  };

  const showUrlDuplicateToast = (video: SourceVideo | null, message?: string) => {
    const name = video?.name?.trim() || '';
    const key = `source-video-url-exists:${video?.id || video?.live_url || name || 'unknown'}`;
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

    try {
      const response = await createSourceVideo({
        name: values.name.trim(),
        live_url: values.liveUrl.trim(),
        remark: values.remark?.trim(),
      });

      if (response.code !== 0) {
        if (isSourceVideoUrlDuplicateError(response)) {
          showUrlDuplicateToast(readDuplicateSourceVideo(response.data), response.message);
          return;
        }
        toast.notify.error(response.message || '添加失败');
        return;
      }

      toast.notify.success('源视频已添加，正在进行 ASR 转写');
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
      <Form form={form} layout="vertical" onFinish={handleSubmit}>
        <Form.Item
          name="name"
          label="直播名称"
          rules={[{ required: true, whitespace: true, message: '请输入直播名称' }]}
        >
          <Input placeholder="请输入直播源名称" maxLength={128} allowClear />
        </Form.Item>

        <Form.Item
          name="liveUrl"
          label="直播地址"
          rules={[
            { required: true, whitespace: true, message: '请输入直播地址' },
            {
              validator: (_, value: string | undefined) => {
                const trimmed = value?.trim();
                if (!trimmed) return Promise.resolve();
                if (/^(https?|rtmp):\/\/.+/i.test(trimmed)) {
                  return Promise.resolve();
                }
                return Promise.reject(new Error('请输入有效的直播地址（http/https/rtmp）'));
              },
            },
          ]}
        >
          <Input placeholder="请输入直播流地址，如 rtmp:// 或 https://" maxLength={512} allowClear />
        </Form.Item>

        <Form.Item name="remark" label="备注">
          <Input placeholder="选填，便于后续搜索识别" maxLength={64} allowClear />
        </Form.Item>
      </Form>
    </Modal>
  );
};

export default AddSourceVideoModal;
