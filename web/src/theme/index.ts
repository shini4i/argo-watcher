import type { PaletteMode, ThemeOptions } from '@mui/material';
import { createTheme, lighten } from '@mui/material/styles';
import { tokens } from './tokens';

/**
 * Reuses the legacy UI branding tokens so both frontends stay visually aligned.
 */
/** Derives the design tokens (palette, typography, overrides) for the requested palette mode. */
const getDesignTokens = (mode: PaletteMode): ThemeOptions => ({
  palette: {
    mode,
    primary: {
      main: mode === 'light' ? tokens.ink : tokens.accent,
    },
    secondary: {
      main: '#ff9800',
    },
    ...(mode === 'dark'
      ? {
          background: {
            default: tokens.canvasDark,
            paper: tokens.surfaceDark,
          },
          text: {
            primary: tokens.textPrimaryDark,
            secondary: tokens.textSecondaryDark,
            disabled: tokens.textDisabledDark,
          },
          divider: tokens.dividerDark,
        }
      : {
          background: {
            default: tokens.surface,
            paper: tokens.surface,
          },
          text: {
            primary: tokens.textPrimary,
            secondary: tokens.textSecondary,
            disabled: tokens.textDisabled,
          },
          divider: tokens.divider,
        }),
    neutral: {
      main: 'gray',
    },
    reason_color: {
      main: lighten('#ff9800', 0.5),
    },
  } as ThemeOptions['palette'],
  typography: {
    fontFamily: tokens.fontSans,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          backgroundColor: mode === 'light' ? tokens.surface : tokens.canvasDark,
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
export { tokens } from './tokens';
