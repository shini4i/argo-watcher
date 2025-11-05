import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { PaletteMode, ThemeOptions } from '@mui/material';
import CssBaseline from '@mui/material/CssBaseline';
import { ThemeProvider, createTheme, lighten } from '@mui/material/styles';

interface ThemeModeContextValue {
  mode: PaletteMode;
  toggleMode: () => void;
}

const STORAGE_KEY = 'argo-watcher-theme-mode';

const ThemeModeContext = createContext<ThemeModeContextValue | undefined>(undefined);

/**
 * Builds MUI design tokens for the provided palette mode.
 * Keeps palette keys like `neutral` and `reason_color` aligned with existing usage.
 */
const getDesignTokens = (mode: PaletteMode): ThemeOptions => ({
  palette: {
    mode,
    primary: {
      main: mode === 'light' ? '#2E3B55' : '#5b7cfa',
    },
    secondary: {
      main: '#ff9800',
    },
    ...(mode === 'dark'
      ? {
          background: {
            default: '#0b1120',
            paper: '#15213b',
          },
          text: {
            primary: '#e2e8f0',
            secondary: '#cbd5f5',
          },
          divider: 'rgba(148, 163, 184, 0.38)',
        }
      : {
          background: {
            default: '#ffffff',
            paper: '#ffffff',
          },
          divider: 'rgba(46, 59, 85, 0.12)',
        }),
    neutral: {
      main: 'gray',
    },
    reason_color: {
      main: lighten('#ff9800', 0.5),
    },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          backgroundColor: mode === 'light' ? '#ffffff' : '#0b1120',
          transition: 'background-color 0.3s ease, color 0.3s ease',
        },
        '#root': {
          minHeight: '100%',
          backgroundColor: 'inherit',
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: {
          transition: 'background-color 0.3s ease, color 0.3s ease, border-color 0.3s ease',
        },
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          backgroundImage: 'none',
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        root: {
          padding: '12px',
        },
      },
    },
    MuiAppBar: {
      styleOverrides: {
        colorPrimary:
          mode === 'dark'
            ? {
                backgroundImage: 'linear-gradient(135deg, #1f2937 0%, #111827 100%)',
              }
            : {
                backgroundImage: 'none',
              },
      },
    },
  },
});

/**
 * Supplies the application with a persisted light/dark theme.
 * The initial mode is loaded from localStorage or falls back to the system preference.
 */
export const ThemeModeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [mode, setMode] = useState<PaletteMode>(() => {
    if (typeof window !== 'undefined') {
      const storedMode = window.localStorage.getItem(STORAGE_KEY);
      if (storedMode === 'light' || storedMode === 'dark') {
        return storedMode;
      }

      if (window.matchMedia) {
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        if (prefersDark) {
          return 'dark';
        }
      }
    }

    return 'light';
  });

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, mode);
    }

    if (typeof document !== 'undefined') {
      document.documentElement.dataset.themeMode = mode;
    }
  }, [mode]);

  const toggleMode = useMemo(
    () => () => {
      setMode(prevMode => (prevMode === 'light' ? 'dark' : 'light'));
    },
    [],
  );

  const contextValue = useMemo(
    () => ({
      mode,
      toggleMode,
    }),
    [mode, toggleMode],
  );

  const theme = useMemo(() => createTheme(getDesignTokens(mode)), [mode]);

  return (
    <ThemeModeContext.Provider value={contextValue}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </ThemeModeContext.Provider>
  );
};

/**
 * Convenience hook for accessing the current palette mode controls.
 */
export const useThemeMode = (): ThemeModeContextValue => {
  const context = useContext(ThemeModeContext);

  if (!context) {
    throw new Error('useThemeMode must be used within a ThemeModeProvider');
  }

  return context;
};
