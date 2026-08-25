declare module '*.css';
declare module '*.svg';
declare module '*.png';
declare module '*.jpg';
declare module '*.jpeg';
declare module '*.gif';
declare module '*.bmp';
declare module '*.tiff';
declare module 'mockjs';

declare module 'react-gtm-module' {
  interface TagManagerArgs {
    gtmId: string;
    dataLayer?: Record<string, unknown>;
    auth?: string;
    preview?: string;
  }

  interface DataLayerArgs {
    dataLayer: Record<string, unknown>;
  }

  const TagManager: {
    initialize(args: TagManagerArgs): void;
    dataLayer(args: DataLayerArgs): void;
  };

  export default TagManager;
}

interface Window {
  dataLayer: Array<{
    event: string;
    [key: string]: unknown;
  }>;
}
