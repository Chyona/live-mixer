import { Flex, Button, Result } from 'antd';
import { useNavigate } from 'react-router-dom';
import { useAppSEO } from '~/hooks/useAppSEO';
import { DEFAULT_APP_PATH } from '~/routes/const';

const ErrorPage = () => {
  const navigate = useNavigate();

  useAppSEO({
    title: '访问出错',
    path: '/error',
    description: '抱歉，访问出现了错误。请稍后重试或返回首页继续浏览。',
    robots: 'noindex, nofollow',
    twitterCard: 'summary',
  });

  return (
    <Flex className="size-full" justify="center" align="center" vertical>
      <Result
        status="error"
        title="访问出错"
        subTitle="抱歉，访问出现了错误，请稍后重试。"
        extra={
          <Button type="primary" onClick={() => navigate(DEFAULT_APP_PATH)}>
            返回
          </Button>
        }
      />
    </Flex>
  );
};

export default ErrorPage;
