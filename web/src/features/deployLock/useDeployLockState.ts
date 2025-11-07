import { useDeployLock } from './DeployLockProvider';

/** Returns the current deploy-lock status from the shared context. */
export const useDeployLockState = () => {
  const { locked } = useDeployLock();
  return locked;
};
