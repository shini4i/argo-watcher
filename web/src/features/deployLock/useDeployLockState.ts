import { useDeployLock } from './DeployLockProvider';

export const useDeployLockState = () => {
  const { locked } = useDeployLock();
  return locked;
};
