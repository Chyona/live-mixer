import { Modal } from 'antd';
import Logo_src from '~/assets/images/logo.png';
import PasswordLogin from './password';
import { closeLogin } from '~/utils/loginFlow';
import { useLoginModalStore } from './store';
import { appConfig } from '~/utils/config';

import './index.css';

interface LoginModalProps {
  closeIcon?: boolean;
  mask?: boolean;
}

const LoginModal = (props?: LoginModalProps) => {
  const { open } = useLoginModalStore();
  const { closeIcon, mask = true } = props ?? {};

  return (
    <Modal
      className="noanimation-modal"
      zIndex={1000}
      width={420}
      open={open}
      centered
      mask={mask}
      closeIcon={closeIcon === false ? null : true}
      maskClosable={false}
      onCancel={() => closeLogin()}
      footer={null}
    >
      <div className="login-brand">
        <img src={Logo_src} className="login-logo" alt="logo" />
        <h2 className="login-brand-text">{appConfig.title}</h2>
      </div>
      <p className="login-subtitle">欢迎登录 {appConfig.title}</p>
      <div className="login-content-wrapper">
        <PasswordLogin />
      </div>
    </Modal>
  );
};

export default LoginModal;
