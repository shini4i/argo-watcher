import { useArgocdStatus } from './ArgocdStatusProvider';

/** Returns true when argo-watcher cannot currently reach ArgoCD. */
export const useArgocdUnreachable = () => {
  const { available } = useArgocdStatus();
  return !available;
};
