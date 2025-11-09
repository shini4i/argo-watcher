import type { ReactNode } from 'react';
import { ThemeModeProvider } from '../../theme/ThemeModeProvider';
import { DeployLockProvider } from '../../features/deployLock/DeployLockProvider';
import { DeployLockBanner } from '../../features/deployLock/DeployLockBanner';
import { TimezoneProvider } from './TimezoneProvider';

interface AppProvidersProps {
  children: ReactNode;
}

/**
 * Hosts global React providers required by the React-admin workspace.
 * Future phases will extend this with Keycloak, deploy-lock, and notification contexts.
 */
export const AppProviders = ({ children }: AppProvidersProps) => (
  <ThemeModeProvider>
    <TimezoneProvider>
      <DeployLockProvider>
        {children}
        <DeployLockBanner />
      </DeployLockProvider>
    </TimezoneProvider>
  </ThemeModeProvider>
);
