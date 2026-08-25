import { Flex, Modal } from 'antd';
import { FaHeadset, FaWeixin } from 'react-icons/fa';
import { appConfig } from '~/utils/config';

const UsModal = ({ open, onCancel }: { open: boolean; onCancel: () => void }) => {
  const hasQrcode = !!appConfig.contactQrcodeUrl;

  return (
    <Modal
      className="contact-us-modal"
      open={open}
      footer={null}
      onCancel={onCancel}
      zIndex={15000}
    >
      <div className="contact-us-modal-title">{appConfig.supportTitle}</div>
      {hasQrcode && (
        <>
          <div className="contact-us-modal-subtitle">
            <FaWeixin className="tip-icon" size={24} color="#15ba11" /> 微信扫码添加咨询
          </div>
          <img className="contact-us-modal-image" src={appConfig.contactQrcodeUrl} alt="contact qrcode" />
        </>
      )}
      <div className="contact-us-modal-description">
        <h3>联系我们</h3>
        <ul>
          <li>
            <FaHeadset />
            产品使用与技术支持
          </li>
          {hasQrcode && (
            <li>
              <FaWeixin />
              微信扫码快速沟通
            </li>
          )}
        </ul>
        {hasQrcode && (
          <Flex className="contact-us-modal-cta" align="center" justify="center" gap={4}>
            <FaWeixin className="tip-icon" size={24} color="#15ba11" />
            立即扫码联系我们
          </Flex>
        )}
      </div>
    </Modal>
  );
};

export default UsModal;
