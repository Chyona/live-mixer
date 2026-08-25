export enum GtmEvents {
  PAGE_VIEW = 'page_view',
  BUTTON_CLICK = 'button_click',
  FORM_SUBMIT = 'form_submit',
  USER_SIGNUP = 'user_signup',
}

export type GtmEventParams = {
  [GtmEvents.PAGE_VIEW]: { page_path: string };
  [GtmEvents.BUTTON_CLICK]: { button_id: string };
  // 添加其他事件参数类型...
};
