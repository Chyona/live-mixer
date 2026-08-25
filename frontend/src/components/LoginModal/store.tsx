import { proxy, useSnapshot } from 'valtio';

const store = proxy({
  open: false,
  returnTo: null as string | null,
});

export const useLoginModalStore = () => useSnapshot(store);
export const getLoginModalStore = () => store;
