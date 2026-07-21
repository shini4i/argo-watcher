import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { argocdStatusService } from './argocdStatusService';

export interface ArgocdStatusContextValue {
  /** True when argo-watcher can currently reach ArgoCD. */
  available: boolean;
}

const ArgocdStatusContext = createContext<ArgocdStatusContextValue | undefined>(undefined);

/** Provides ArgoCD reachability state backed by the shared WebSocket service. */
export const ArgocdStatusProvider = ({ children }: { children: ReactNode }) => {
  // Default to available so the banner never flashes before the initial fetch
  // resolves; the service corrects this within one round-trip.
  const [available, setAvailable] = useState(true);

  useEffect(() => {
    const unsubscribe = argocdStatusService.subscribe(status => {
      setAvailable(status);
    });
    return () => unsubscribe();
  }, []);

  const value = useMemo<ArgocdStatusContextValue>(() => ({ available }), [available]);

  return <ArgocdStatusContext.Provider value={value}>{children}</ArgocdStatusContext.Provider>;
};

/** Hook exposing ArgoCD reachability context. */
export const useArgocdStatus = (): ArgocdStatusContextValue => {
  const context = useContext(ArgocdStatusContext);
  if (!context) {
    throw new Error('useArgocdStatus must be used within an ArgocdStatusProvider');
  }
  return context;
};
