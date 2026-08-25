import { Flex, Layout } from 'antd';
import NavModule from '../Nav';
import Logo_src from '~/assets/images/logo.png';
import { appConfig } from '~/utils/config';
import { DEFAULT_APP_PATH } from '~/routes/const';

import './index.css';
import { navigateTo } from '~/utils/navigation';

const { Header } = Layout;

export default () => {
  return (
    <Header className="zt-header">
      <Flex className="size-full" gap={8} align="center" justify="space-between">
        <div
          className="flex items-center gap-2 ml-4 mr-2 cursor-pointer"
          onClick={() => navigateTo(DEFAULT_APP_PATH)}
        >
          <img src={Logo_src} className="ml-2 h-[32px]" alt="logo" />
          <span className="brand-logo">{appConfig.title}</span>
        </div>
        <NavModule />
      </Flex>
    </Header>
  );
};
