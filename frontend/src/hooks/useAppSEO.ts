import { useSEO, type SEOProps } from './useSEO';
import { appConfig } from '~/utils/config';

type AppSEOOptions = Partial<Omit<SEOProps, 'author' | 'ogUrl'>> & {
  title: string;
  path?: string;
};

export const useAppSEO = ({ title, path = '', ...rest }: AppSEOOptions) => {
  const pageTitle = `${title} | ${appConfig.title}`;
  const pageUrl = `${appConfig.siteUrl.replace(/\/$/, '')}${path.startsWith('/') ? path : `/${path}`}`;

  useSEO({
    author: appConfig.companyName,
    ogUrl: pageUrl,
    title: pageTitle,
    ogTitle: pageTitle,
    twitterTitle: pageTitle,
    ...rest,
  });
};

export default useAppSEO;
