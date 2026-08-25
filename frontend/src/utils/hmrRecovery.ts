let pendingRecovery = false;

export function markHmrRecoveryPending() {
  pendingRecovery = true;
}

export function consumeHmrRecoveryPending() {
  const shouldRecover = pendingRecovery;
  pendingRecovery = false;
  return shouldRecover;
}
