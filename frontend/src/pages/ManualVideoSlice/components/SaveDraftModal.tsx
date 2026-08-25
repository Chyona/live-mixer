import { Form, Input, Modal } from 'antd';

export interface SaveDraftModalValues {
  name: string;
  remark: string;
}

interface SaveDraftModalProps {
  open: boolean;
  title: string;
  defaultName: string;
  defaultRemark?: string;
  /** 是否展示备注字段（新建 / 另存为需要） */
  showRemark?: boolean;
  loading: boolean;
  onCancel: () => void;
  onSubmit: (values: SaveDraftModalValues) => void;
}

const SaveDraftModal = ({
  open,
  title,
  defaultName,
  defaultRemark = '',
  showRemark = false,
  loading,
  onCancel,
  onSubmit,
}: SaveDraftModalProps) => {
  const [form] = Form.useForm<SaveDraftModalValues>();

  return (
    <Modal
      className="noanimation-modal"
      open={open}
      title={title}
      okText="确定"
      cancelText="取消"
      confirmLoading={loading}
      destroyOnClose
      onCancel={() => {
        form.resetFields();
        onCancel();
      }}
      onOk={() => {
        void form.validateFields().then((values) => {
          onSubmit({
            name: values.name.trim(),
            remark: (values.remark ?? '').trim(),
          });
          form.resetFields();
        });
      }}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={{ name: defaultName, remark: defaultRemark }}
        key={`${open}-${defaultName}-${defaultRemark}-${showRemark}`}
      >
        <Form.Item
          label="项目名称"
          name="name"
          rules={[{ required: true, message: '请输入项目名称' }]}
        >
          <Input placeholder="请输入项目名称" maxLength={50} />
        </Form.Item>
        {showRemark ? (
          <Form.Item label="项目备注" name="remark">
            <Input.TextArea placeholder="请输入项目备注（可选）" maxLength={200} rows={3} />
          </Form.Item>
        ) : null}
      </Form>
    </Modal>
  );
};

export default SaveDraftModal;
