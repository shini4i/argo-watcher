import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { deployLockService } from './deployLockService';

export interface DeployLockContextValue {
  locked: boolean;
  setLock: () => Promise<void>;
  releaseLock: () => Promise<void>;
}

const DeployLockContext = createContext<DeployLockContextValue | undefined>(undefined);

/** Provides deploy-lock state and actions backed by the shared WebSocket service. */
export const DeployLockProvider = ({ children }: { children: ReactNode }) => {
  const [locked, setLocked] = useState(false);

  useEffect(() => {
    const unsubscribe = deployLockService.subscribe(status => {
      setLocked(status);
    });
    return () => unsubscribe();
  }, []);

  const value = useMemo<DeployLockContextValue>(
    () => ({
      locked,
      setLock: async () => {
        await deployLockService.setLock();
      },
      releaseLock: async () => {
        await deployLockService.releaseLock();
      },
    }),
    [locked],
  );

  return <DeployLockContext.Provider value={value}>{children}</DeployLockContext.Provider>;
};

/** Hook exposing DeployLock context values (state + actions). */
export const useDeployLock = (): DeployLockContextValue => {
  const context = useContext(DeployLockContext);
  if (!context) {
    throw new Error('useDeployLock must be used within a DeployLockProvider');
  }
  return context;
};
