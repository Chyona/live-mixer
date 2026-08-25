import { Flex, Button, Result } from 'antd';
import { useNavigate } from 'react-router-dom';
import { useAppSEO } from '~/hooks/useAppSEO';
import { DEFAULT_APP_PATH } from '~/routes/const';

const NotFoundPage = () => {
  const navigate = useNavigate();

  useAppSEO({
    title: '404 页面未找到',
    path: '/404',
    description: '抱歉，您访问的页面不存在。',
    robots: 'noindex, nofollow',
    twitterCard: 'summary',
  });

  return (
    <Flex className="size-full" justify="center" align="center" vertical>
      <Result
        status="404"
        title="404"
        subTitle="抱歉，你访问的页面不存在。"
        extra={
          <Button type="primary" onClick={() => navigate(DEFAULT_APP_PATH)}>
            返回
          </Button>
        }
      />
    </Flex>
  );
};

export default NotFoundPage;
