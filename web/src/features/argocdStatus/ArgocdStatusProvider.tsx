import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { argocdStatusService, type ArgocdStatus, type ArgocdUnavailableReason } from './argocdStatusService';

export interface ArgocdStatusContextValue {
  /** True when argo-watcher can currently reach ArgoCD and its state backend. */
  available: boolean;
  /** Which subsystem is unreachable when `available` is false; null otherwise. */
  reason: ArgocdUnavailableReason;
}

const ArgocdStatusContext = createContext<ArgocdStatusContextValue | undefined>(undefined);

/** Provides ArgoCD reachability state backed by the shared WebSocket service. */
export const ArgocdStatusProvider = ({ children }: { children: ReactNode }) => {
  // Default to available so the banner never flashes before the initial fetch
  // resolves; the service corrects this within one round-trip.
  const [status, setStatus] = useState<ArgocdStatus>({ available: true, reason: null });

  useEffect(() => {
    const unsubscribe = argocdStatusService.subscribe(next => {
      setStatus(next);
    });
    return () => unsubscribe();
  }, []);

  const value = useMemo<ArgocdStatusContextValue>(
    () => ({ available: status.available, reason: status.reason }),
    [status],
  );

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
