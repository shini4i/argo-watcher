import { useArgocdStatus } from './ArgocdStatusProvider';

/** Returns true when argo-watcher cannot currently reach ArgoCD or its state backend. */
export const useArgocdUnreachable = () => {
  const { available } = useArgocdStatus();
  return !available;
};
