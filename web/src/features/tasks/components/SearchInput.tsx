import { useEffect, useState } from 'react';
import { InputAdornment, TextField } from '@mui/material';
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
 */
export const SearchInput = ({
  value,
  onChange,
  placeholder = 'Search…',
  debounceMs = 200,
}: SearchInputProps) => {
  const theme = useTheme();
  const [draft, setDraft] = useState(value);
  const [focused, setFocused] = useState(false);
  const [pauseActive, setPauseActive] = useState(false);

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

  usePauseRefresh('search', pauseActive);

  return (
    <TextField
      size="small"
      value={draft}
      onChange={event => setDraft(event.target.value)}
      placeholder={placeholder}
      inputProps={{
        'aria-label': 'Search tasks',
        onFocus: () => setFocused(true),
        onBlur: () => setFocused(false),
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
