import { useEffect } from 'react';

export interface SEOProps {
  title?: string;
  description?: string;
  keywords?: string;
  author?: string;
  robots?: string;
  ogTitle?: string;
  ogDescription?: string;
  ogImage?: string;
  ogUrl?: string;
  twitterCard?: string;
  twitterTitle?: string;
  twitterDescription?: string;
  twitterImage?: string;
}

export const useSEO = ({
  title,
  description,
  keywords,
  author,
  robots,
  ogTitle,
  ogDescription,
  ogImage,
  ogUrl,
  twitterCard,
  twitterTitle,
  twitterDescription,
  twitterImage,
}: SEOProps) => {
  useEffect(() => {
    const metaTags = [
      { name: 'description', content: description },
      { name: 'keywords', content: keywords },
      { name: 'author', content: author },
      { name: 'robots', content: robots || 'index, follow' },
      { property: 'og:title', content: ogTitle || title },
      { property: 'og:description', content: ogDescription || description },
      { property: 'og:image', content: ogImage },
      { property: 'og:url', content: ogUrl },
      { name: 'twitter:card', content: twitterCard },
      { name: 'twitter:title', content: twitterTitle || title },
      { name: 'twitter:description', content: twitterDescription || description },
      { name: 'twitter:image', content: twitterImage },
    ];

    const head = document.head;

    // Update or create meta tags
    metaTags.forEach((tag) => {
      if (!tag.content) return;

      const selector = tag.name ? `meta[name="${tag.name}"]` : `meta[property="${tag.property}"]`;

      let element = document.querySelector(selector) as HTMLMetaElement;

      if (!element) {
        element = document.createElement('meta');
        if (tag.name) {
          element.setAttribute('name', tag.name);
        } else if (tag.property) {
          element.setAttribute('property', tag.property);
        }
        head.appendChild(element);
      }

      element.setAttribute('content', tag.content);
    });

    // Update title
    if (title) {
      document.title = title;
    }

    // Cleanup function
    return () => {
      // We don't remove the meta tags on cleanup to avoid flickering
      // when navigating between pages with similar SEO settings
    };
  }, [
    title,
    description,
    keywords,
    author,
    robots,
    ogTitle,
    ogDescription,
    ogImage,
    ogUrl,
    twitterCard,
    twitterTitle,
    twitterDescription,
    twitterImage,
  ]);
};

export default useSEO;
