import type { ReactNode } from 'react';
import { ThemeModeProvider } from '../../theme/ThemeModeProvider';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';
import { DeployLockBanner } from '../../features/deployLock/DeployLockBanner';
import { ArgocdStatusProvider } from '../../features/argocdStatus/ArgocdStatusProvider';
import { ArgocdUnreachableBanner } from '../../features/argocdStatus/ArgocdUnreachableBanner';
import { TimezoneProvider } from './TimezoneProvider';

interface AppProvidersProps {
  children: ReactNode;
}

/**
 * Hosts the global React providers (theme, timezone, ArgoCD-status, deploy-lock)
 * that wrap the React-admin workspace.
 */
export const AppProviders = ({ children }: AppProvidersProps) => (
  <ThemeModeProvider>
    <TimezoneProvider>
      <ArgocdStatusProvider>
        <DeployLockProvider>
          {children}
          <DeployLockBanner />
          <ArgocdUnreachableBanner />
        </DeployLockProvider>
      </ArgocdStatusProvider>
    </TimezoneProvider>
  </ThemeModeProvider>
);
