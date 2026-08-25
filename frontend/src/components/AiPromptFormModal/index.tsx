import { Button, Form, Input, Modal } from 'antd';
import { useEffect, useState } from 'react';
import { LuMaximize2, LuMinimize2 } from 'react-icons/lu';

import { AppError } from '~/services/http';
import { createAiPrompt, updateAiPrompt, type AiPrompt } from '~/services/aiPrompt';
import { showAppError, toast } from '~/utils/toast';

import './index.css';

type FormValues = {
  name: string;
  content: string;
  remark?: string;
};

interface AiPromptFormModalProps {
  open: boolean;
  prompt?: AiPrompt | null;
  onClose: () => void;
  onSuccess: (prompt: AiPrompt) => void;
}

const AiPromptFormModal = ({ open, prompt, onClose, onSuccess }: AiPromptFormModalProps) => {
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const isEdit = Boolean(prompt);

  useEffect(() => {
    if (!open) {
      setExpanded(false);
      return;
    }

    if (prompt) {
      form.setFieldsValue({
        name: prompt.name,
        content: prompt.content,
        remark: prompt.remark,
      });
      return;
    }

    form.resetFields();
  }, [form, open, prompt]);

  const handleClose = () => {
    form.resetFields();
    setExpanded(false);
    onClose();
  };

  const handleSubmit = async (values: FormValues) => {
    setSubmitting(true);

    try {
      const payload = {
        name: values.name.trim(),
        content: values.content?.trim(),
        remark: values.remark?.trim(),
      };

      const response = isEdit && prompt
        ? await updateAiPrompt(prompt.id, payload)
        : await createAiPrompt(payload);

      if (response.code !== 0) {
        toast.notify.error(response.message || `${isEdit ? '保存' : '添加'}失败`);
        return;
      }

      toast.notify.success(isEdit ? '提示词已保存' : '提示词已添加');
      handleClose();
      onSuccess(response.data);
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error(`${isEdit ? '保存' : '添加'}失败`);
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      className={`noanimation-modal ai-prompt-form-modal${expanded ? ' ai-prompt-form-modal_expanded' : ''}`}
      title={isEdit ? '编辑提示词' : '添加提示词'}
      width={expanded ? 'min(1200px, 96vw)' : 'min(960px, 94vw)'}
      centered
      open={open}
      okText={isEdit ? '保存' : '添加'}
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnClose
      onCancel={handleClose}
      onOk={() => form.submit()}
    >
      <Form form={form} layout="vertical" onFinish={handleSubmit}>
        <div className="ai-prompt-form-modal__meta-row">
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, whitespace: true, message: '请输入名称' }]}
          >
            <Input placeholder="请输入提示词名称" maxLength={64} allowClear />
          </Form.Item>

          <Form.Item name="remark" label="备注">
            <Input placeholder="选填，便于后续搜索识别" maxLength={128} allowClear />
          </Form.Item>
        </div>

        <Form.Item
          name="content"
          label={
            <span className="ai-prompt-form-modal__content-label">
              <span>
                提示词信息
                <span className="ai-prompt-form-modal__content-label-hint">（支持较长文本）</span>
              </span>
              <Button
                type="link"
                size="small"
                className="ai-prompt-form-modal__expand-btn"
                icon={expanded ? <LuMinimize2 size={14} /> : <LuMaximize2 size={14} />}
                onClick={() => setExpanded((prev) => !prev)}
              >
                {expanded ? '收起编辑' : '全屏编辑'}
              </Button>
            </span>
          }
          rules={[{ required: true, whitespace: true, message: '请输入提示词信息' }]}
        >
          <Input.TextArea
            className="ai-prompt-form-modal__content"
            placeholder="请输入 AI 提示词内容"
            maxLength={6000}
            showCount
          />
        </Form.Item>
      </Form>
    </Modal>
  );
};

export default AiPromptFormModal;
