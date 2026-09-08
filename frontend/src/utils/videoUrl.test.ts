import { describe, expect, it } from 'vitest';
import { isSameMediaResource, mediaResourceKey, resolveVideoCrossOrigin } from './videoUrl';

describe('resolveVideoCrossOrigin', () => {
  const origin = 'https://app.example.com';

  it('同源相对路径不设置 crossOrigin', () => {
    expect(resolveVideoCrossOrigin('/tos-media/video.mp4', origin)).toBe('');
  });

  it('跨域 https 视频使用 anonymous', () => {
    expect(
      resolveVideoCrossOrigin('https://bucket.tos-cn-shanghai.volces.com/a.mp4', origin)
    ).toBe('anonymous');
  });

  it('blob 与 data 不设置 crossOrigin', () => {
    expect(resolveVideoCrossOrigin('blob:https://app.example.com/uuid', origin)).toBe('');
    expect(resolveVideoCrossOrigin('data:video/mp4;base64,abc', origin)).toBe('');
  });
});

describe('mediaResourceKey', () => {
  it('忽略对象存储签名 query', () => {
    const a =
      'https://bucket.cos.ap-guangzhou.myqcloud.com/video_editing/live-record/uuid/live.m3u8?q-sign-algorithm=sha1&q-sign-time=1';
    const b =
      'https://bucket.cos.ap-guangzhou.myqcloud.com/video_editing/live-record/uuid/live.m3u8?q-sign-algorithm=sha1&q-sign-time=2';
    expect(mediaResourceKey(a)).toBe(mediaResourceKey(b));
    expect(isSameMediaResource(a, b)).toBe(true);
  });

  it('不同对象不算同一资源', () => {
    expect(
      isSameMediaResource(
        'https://cdn.example/live.m3u8?sig=1',
        'https://cdn.example/other.m3u8?sig=1'
      )
    ).toBe(false);
  });
});
