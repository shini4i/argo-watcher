import { CssBaseline, PaletteMode } from '@mui/material';
import { ThemeProvider, Theme } from '@mui/material/styles';
import type { ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { createAppTheme } from '.';

interface ThemeModeContextValue {
  mode: PaletteMode;
  toggleMode: () => void;
  theme: Theme;
}

const STORAGE_KEY = 'argo-watcher.theme-mode';

const ThemeModeContext = createContext<ThemeModeContextValue | undefined>(undefined);

const resolveWindow = () => {
  const maybeWindow = (globalThis as typeof globalThis & { window?: Window }).window;
  return maybeWindow ?? undefined;
};

const resolveDocument = () => {
  const maybeDocument = (globalThis as typeof globalThis & { document?: Document }).document;
  return maybeDocument ?? undefined;
};

const readInitialMode = (): PaletteMode => {
  const browserWindow = resolveWindow();
  if (browserWindow) {
    const stored = browserWindow.localStorage.getItem(STORAGE_KEY);
    if (stored === 'light' || stored === 'dark') {
      return stored;
    }

    if (browserWindow.matchMedia?.('(prefers-color-scheme: dark)').matches) {
      return 'dark';
    }
  }

  return 'light';
};

export const ThemeModeProvider = ({ children }: { children: ReactNode }) => {
  const [mode, setMode] = useState<PaletteMode>(() => readInitialMode());

  useEffect(() => {
    const browserWindow = resolveWindow();
    if (browserWindow) {
      browserWindow.localStorage.setItem(STORAGE_KEY, mode);
    }

    const browserDocument = resolveDocument();
    if (browserDocument) {
      browserDocument.documentElement.dataset.themeMode = mode;
    }
  }, [mode]);

  const toggleMode = useCallback(() => {
    setMode(prev => (prev === 'light' ? 'dark' : 'light'));
  }, []);

  const theme = useMemo(() => createAppTheme(mode), [mode]);

  const value = useMemo<ThemeModeContextValue>(
    () => ({
      mode,
      toggleMode,
      theme,
    }),
    [mode, toggleMode, theme],
  );

  return (
    <ThemeModeContext.Provider value={value}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </ThemeModeContext.Provider>
  );
};

export const useThemeMode = (): ThemeModeContextValue => {
  const context = useContext(ThemeModeContext);
  if (!context) {
    throw new Error('useThemeMode must be used within a ThemeModeProvider');
  }
  return context;
};
