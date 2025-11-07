import type { PaletteMode, ThemeOptions } from '@mui/material';
import { createTheme, lighten } from '@mui/material/styles';

/**
 * Reuses the legacy UI branding tokens so both frontends stay visually aligned.
 */
/** Derives the design tokens (palette, typography, overrides) for the requested palette mode. */
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
  } as ThemeOptions['palette'],
  typography: {
    fontFamily: '"Roboto", "Helvetica", "Arial", sans-serif',
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

/** Convenient wrapper to build the MUI theme based on the current palette mode. */
export const createAppTheme = (mode: PaletteMode) => createTheme(getDesignTokens(mode));

export const lightTheme = createAppTheme('light');
export const darkTheme = createAppTheme('dark');

export { ThemeModeProvider, useThemeMode } from './ThemeModeProvider';
