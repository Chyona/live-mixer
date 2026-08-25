const fs = require('fs');
const path = require('path');
const { SitemapStream, streamToPromise } = require('sitemap');

const baseUrl = process.env.VITE_SITE_URL || 'http://localhost:8008';

function extractRoutesFromTsFile() {
  try {
    const tsFilePath = path.join(__dirname, '../src/routes/const.tsx');
    const tsContent = fs.readFileSync(tsFilePath, 'utf8');
    const routes = [];
    const routeRegex = /{[^}]+}/g;
    const matches = tsContent.match(routeRegex);

    if (matches) {
      matches.forEach((match) => {
        const pathMatch = match.match(/path:\s*['"]([^'"]+)['"]/);
        const hrefMatch = match.match(/href:\s*['"]([^'"]+)['"]/);

        if (pathMatch) {
          const route = { path: pathMatch[1] };
          if (hrefMatch) route.href = hrefMatch[1];
          routes.push(route);
        }
      });
    }

    return routes;
  } catch (error) {
    console.error('读取路由配置文件失败:', error);
    return [{ path: '/source-videos' }];
  }
}

async function generateSitemap() {
  const routes = extractRoutesFromTsFile();
  const sitemap = new SitemapStream({ hostname: baseUrl.replace(/\/$/, '') });

  routes.forEach((route) => {
    if (route.href) return;
    sitemap.write({
      url: route.path === '/' ? '/' : route.path,
      changefreq: 'weekly',
      priority: route.path === '/' ? 1.0 : 0.8,
    });
  });

  sitemap.end();
  const data = await streamToPromise(sitemap);
  fs.writeFileSync(path.join(__dirname, '../public/sitemap.xml'), data.toString());
  console.log('Sitemap generated at public/sitemap.xml');
}

generateSitemap().catch(console.error);
