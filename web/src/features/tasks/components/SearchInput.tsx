import { useEffect, useRef, useState } from 'react';
import { IconButton, InputAdornment, TextField, useMediaQuery } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import SearchIcon from '@mui/icons-material/Search';
import { tokens } from '../../../theme/tokens';
import { usePauseRefresh } from './TaskListContext';

interface SearchInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly placeholder?: string;
  readonly debounceMs?: number;
}

/**
 * Lightweight client-side search input for the toolbar.
 * Debounces user input by 200 ms before bubbling up so callers can filter
 * the loaded page without thrashing. Holds the auto-refresh paused while
 * focused (and briefly after blur) so the list does not reshuffle mid-keystroke.
 *
 * Below 1200 px (or when there is no value to display), the input collapses
 * into a search icon button to keep the toolbar from squeezing other
 * controls; clicking expands it and auto-focuses for typing.
 */
export const SearchInput = ({
  value,
  onChange,
  placeholder = 'Search…',
  debounceMs = 200,
}: SearchInputProps) => {
  const theme = useTheme();
  const isWide = useMediaQuery('(min-width: 1200px)');
  const [draft, setDraft] = useState(value);
  const [focused, setFocused] = useState(false);
  const [pauseActive, setPauseActive] = useState(false);
  const [expanded, setExpanded] = useState(() => isWide || Boolean(value));
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    setDraft(value);
  }, [value]);

  useEffect(() => {
    if (draft === value) {
      return undefined;
    }
    const handle = window.setTimeout(() => onChange(draft), debounceMs);
    return () => window.clearTimeout(handle);
  }, [draft, debounceMs, onChange, value]);

  // Keep refresh paused while focused; release after a short grace period
  // so the trailing debounced onChange does not race a fresh refetch.
  useEffect(() => {
    if (focused) {
      setPauseActive(true);
      return undefined;
    }
    const handle = window.setTimeout(() => setPauseActive(false), debounceMs + 100);
    return () => window.clearTimeout(handle);
  }, [focused, debounceMs]);

  // Keep the expanded/collapsed state in sync with the viewport and the
  // active value. A non-empty value forces expansion so the query is always
  // visible — collapsing it would hide the user's own input. While the user
  // is typing we leave `expanded` alone; otherwise backspacing the last char
  // (value → '') would collapse the input mid-keystroke on narrow viewports.
  useEffect(() => {
    if (focused) return;
    setExpanded(isWide || Boolean(value));
  }, [isWide, value, focused]);

  usePauseRefresh('search', pauseActive);

  if (!expanded) {
    return (
      <IconButton
        aria-label="Open search"
        onClick={() => {
          setExpanded(true);
          // Focus after the TextField mounts.
          requestAnimationFrame(() => inputRef.current?.focus());
        }}
        sx={{
          width: 36,
          height: 34,
          borderRadius: `${tokens.radiusMd}px`,
          border: `1px solid ${theme.palette.divider}`,
          backgroundColor: theme.palette.background.paper,
          color: theme.palette.text.secondary,
          '&:hover': { borderColor: theme.palette.text.secondary },
        }}
      >
        <SearchIcon fontSize="small" />
      </IconButton>
    );
  }

  return (
    <TextField
      size="small"
      value={draft}
      onChange={event => setDraft(event.target.value)}
      placeholder={placeholder}
      inputRef={inputRef}
      inputProps={{
        'aria-label': 'Search tasks',
        onFocus: () => setFocused(true),
        onBlur: () => {
          setFocused(false);
          if (!isWide && !draft) {
            setExpanded(false);
          }
        },
      }}
      InputProps={{
        startAdornment: (
          <InputAdornment position="start">
            <SearchIcon fontSize="small" sx={{ color: theme.palette.text.secondary }} />
          </InputAdornment>
        ),
        sx: { height: 34, borderRadius: `${tokens.radiusMd}px`, fontSize: 13.5 },
      }}
      sx={{ minWidth: 220 }}
    />
  );
};
