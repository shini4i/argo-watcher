import { useEffect, useState } from 'react';
import { InputAdornment, TextField } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import SearchIcon from '@mui/icons-material/Search';
import { tokens } from '../../../theme/tokens';

interface SearchInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly placeholder?: string;
  readonly debounceMs?: number;
}

/**
 * Lightweight client-side search input for the toolbar.
 * Debounces user input by 200 ms before bubbling up so callers can filter
 * the loaded page without thrashing.
 */
export const SearchInput = ({
  value,
  onChange,
  placeholder = 'Search…',
  debounceMs = 200,
}: SearchInputProps) => {
  const theme = useTheme();
  const [draft, setDraft] = useState(value);

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

  return (
    <TextField
      size="small"
      value={draft}
      onChange={event => setDraft(event.target.value)}
      placeholder={placeholder}
      aria-label="Search tasks"
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
