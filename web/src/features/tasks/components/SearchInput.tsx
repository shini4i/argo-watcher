import { useEffect, useRef, useState } from 'react';
import { IconButton, InputAdornment, TextField, useMediaQuery } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import SearchIcon from '@mui/icons-material/Search';
import { tokens } from '../../../theme/tokens';
import { usePauseRefresh } from './TaskListContext';

/** Mirrors the API's own cap; a longer term is rejected with HTTP 400. */
const MAX_QUERY_LENGTH = 255;

// Counted in code points, as the API counts runes. The DOM `maxLength` measures
// UTF-16 code units instead, which would stop a non-BMP term such as an emoji
// at half the length the API accepts.
const clampQuery = (value: string): string => {
  const points = [...value];
  return points.length > MAX_QUERY_LENGTH ? points.slice(0, MAX_QUERY_LENGTH).join('') : value;
};

interface SearchInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly placeholder?: string;
  readonly debounceMs?: number;
}

/**
 * @description Reports the committed query to `onChange`, debounced, since each
 * commit reaches the backend as a filtered task query. Auto-refresh stays paused
 * while focused (and briefly after blur) so the list does not reshuffle
 * mid-keystroke. Below 1200 px the input collapses into an icon button when it
 * has no value, to keep the toolbar from squeezing the other controls.
 */
export const SearchInput = ({
  value,
  onChange,
  placeholder = 'Search…',
  debounceMs = 350,
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
    const handle = globalThis.setTimeout(() => onChange(draft), debounceMs);
    return () => globalThis.clearTimeout(handle);
  }, [draft, debounceMs, onChange, value]);

  // The release is delayed by a grace period so the trailing debounced
  // onChange does not race a fresh refetch.
  useEffect(() => {
    if (focused) {
      setPauseActive(true);
      return undefined;
    }
    const handle = globalThis.setTimeout(() => setPauseActive(false), debounceMs + 100);
    return () => globalThis.clearTimeout(handle);
  }, [focused, debounceMs]);

  // A non-empty value forces expansion so the query stays visible — collapsing
  // it would hide the user's own input. While the user is typing `expanded` is
  // left alone; otherwise backspacing the last char (value → '') would collapse
  // the input mid-keystroke on narrow viewports.
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
      onChange={event => setDraft(clampQuery(event.target.value))}
      placeholder={placeholder}
      inputRef={inputRef}
      slotProps={{
        htmlInput: {
          'aria-label': 'Search tasks',
          onFocus: () => setFocused(true),
          onBlur: () => {
            setFocused(false);
            if (!isWide && !draft) {
              setExpanded(false);
            }
          },
        },
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <SearchIcon fontSize="small" sx={{ color: theme.palette.text.secondary }} />
            </InputAdornment>
          ),
          sx: { height: 34, borderRadius: `${tokens.radiusMd}px`, fontSize: 13.5 },
        },
      }}
      sx={{ minWidth: 220 }}
    />
  );
};
