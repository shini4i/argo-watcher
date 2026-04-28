import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Box,
  Button,
  ButtonBase,
  IconButton,
  Popover,
  Stack,
  Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import CalendarMonthOutlinedIcon from '@mui/icons-material/CalendarMonthOutlined';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import { useTimezone } from '../../../../shared/providers/TimezoneProvider';
import { tokens } from '../../../../theme/tokens';
import {
  PRESETS,
  buildMonthGrid,
  dateAt,
  dayCount,
  endOfDay,
  isSameDay,
  matchPreset,
  startOfDay,
  ymd,
  type DateRangeValue,
} from './calendar';

interface DateRangePickerProps {
  readonly value: DateRangeValue;
  readonly onApply: (next: DateRangeValue) => void;
}

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

const TRIGGER_FORMAT: Intl.DateTimeFormatOptions = {
  month: 'short',
  day: '2-digit',
  year: 'numeric',
};

const formatTriggerLabel = (range: DateRangeValue, formatDate: (ts: number, opts?: Intl.DateTimeFormatOptions) => string) => {
  if (range.start === null || range.end === null) {
    return 'Select date range';
  }
  return `${formatDate(range.start, TRIGGER_FORMAT)} → ${formatDate(range.end, TRIGGER_FORMAT)}`;
};

const MONTH_FORMAT: Intl.DateTimeFormatOptions = { month: 'long', year: 'numeric' };

/**
 * Date range picker with preset shortcuts and a custom Monday-first calendar.
 * `value` is in Unix seconds; `onApply` fires only when the user clicks Apply
 * with a complete and dirty range. Computation honours the active timezone.
 */
export const DateRangePicker = ({ value, onApply }: DateRangePickerProps) => {
  const theme = useTheme();
  const { timezone, formatDate } = useTimezone();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [draft, setDraft] = useState<DateRangeValue>(value);
  const [pickingStart, setPickingStart] = useState(true);
  const [viewYear, setViewYear] = useState(() => ymd(new Date(), timezone).year);
  const [viewMonth, setViewMonth] = useState(() => ymd(new Date(), timezone).month);

  // Sync external value into the popover whenever it (re)opens.
  useEffect(() => {
    if (anchor) {
      setDraft(value);
      setPickingStart(true);
      const anchorDate = value.start == null ? new Date() : new Date(value.start * 1000);
      const focused = ymd(anchorDate, timezone);
      setViewYear(focused.year);
      setViewMonth(focused.month);
    }
  }, [anchor, value, timezone]);

  const open = Boolean(anchor);
  const handleOpen = useCallback((event: React.MouseEvent<HTMLElement>) => {
    setAnchor(event.currentTarget);
  }, []);
  const handleClose = useCallback(() => setAnchor(null), []);

  const handlePresetClick = useCallback(
    (presetId: string) => {
      const preset = PRESETS.find(p => p.id === presetId);
      if (!preset) return;
      setDraft(preset.compute(timezone));
      setPickingStart(true);
    },
    [timezone],
  );

  const handleDayClick = useCallback(
    (date: Date) => {
      const startSec = Math.floor(startOfDay(date, timezone).getTime() / 1000);
      const endSec = Math.floor(endOfDay(date, timezone).getTime() / 1000);

      if (pickingStart || draft.start === null) {
        setDraft({ start: startSec, end: null });
        setPickingStart(false);
        return;
      }

      if (startSec < draft.start) {
        // Swap: clicked date becomes the new start, previous start becomes the end.
        const previousStartDate = new Date(draft.start * 1000);
        setDraft({
          start: startSec,
          end: Math.floor(endOfDay(previousStartDate, timezone).getTime() / 1000),
        });
      } else {
        setDraft({ start: draft.start, end: endSec });
      }
      setPickingStart(true);
    },
    [draft, pickingStart, timezone],
  );

  const matchedPreset = useMemo(() => matchPreset(draft, timezone), [draft, timezone]);

  const grid = useMemo(
    () => buildMonthGrid(viewYear, viewMonth, timezone),
    [viewYear, viewMonth, timezone],
  );

  const draftStartDate = draft.start === null ? null : new Date(draft.start * 1000);
  const draftEndDate = draft.end === null ? null : new Date(draft.end * 1000);

  const goPrevMonth = () => {
    if (viewMonth === 0) {
      setViewMonth(11);
      setViewYear(viewYear - 1);
    } else {
      setViewMonth(viewMonth - 1);
    }
  };
  const goNextMonth = () => {
    if (viewMonth === 11) {
      setViewMonth(0);
      setViewYear(viewYear + 1);
    } else {
      setViewMonth(viewMonth + 1);
    }
  };

  const isDirty =
    draft.start !== value.start || draft.end !== value.end;
  const isComplete = draft.start !== null && draft.end !== null;
  const canApply = isDirty && isComplete;

  const span = isComplete && draft.start !== null && draft.end !== null
    ? dayCount(draft.start, draft.end)
    : 0;
  const dayWord = span === 1 ? 'day' : 'days';
  const spanLabel = span > 0 ? ` · ${span} ${dayWord} selected` : '';

  // formatDate merges over DEFAULT_DATE_FORMAT, which leaks day/hour/minute
  // into the header. Use Intl directly so we get just "April 2026".
  const monthHeader = useMemo(
    () =>
      new Intl.DateTimeFormat('en-GB', {
        ...MONTH_FORMAT,
        timeZone: timezone === 'utc' ? 'UTC' : undefined,
      }).format(dateAt(viewYear, viewMonth, 1, timezone)),
    [viewYear, viewMonth, timezone],
  );

  return (
    <>
      <Button
        variant="outlined"
        startIcon={<CalendarMonthOutlinedIcon fontSize="small" />}
        endIcon={<KeyboardArrowDownIcon fontSize="small" />}
        onClick={handleOpen}
        sx={{
          height: 36,
          borderRadius: `${tokens.radiusMd}px`,
          textTransform: 'none',
          fontSize: 13,
          color: 'text.primary',
          borderColor: theme.palette.divider,
          backgroundColor: theme.palette.background.paper,
          '&:hover': { borderColor: theme.palette.text.secondary, backgroundColor: theme.palette.background.paper },
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        {formatTriggerLabel(value, formatDate)}
      </Button>
      <Popover
        open={open}
        anchorEl={anchor}
        onClose={handleClose}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        slotProps={{
          paper: {
            sx: {
              width: 540,
              borderRadius: `${tokens.radiusMd}px`,
              border: `1px solid ${theme.palette.divider}`,
              boxShadow: '0 18px 40px rgba(15, 23, 42, 0.18)',
              overflow: 'hidden',
            },
          },
        }}
      >
        <Stack direction="row" sx={{ minHeight: 320 }}>
          <Stack
            sx={{
              width: 160,
              backgroundColor:
                theme.palette.mode === 'dark' ? tokens.surface2Dark : tokens.surface2,
              padding: '10px 8px',
              gap: '2px',
              borderRight: `1px solid ${theme.palette.divider}`,
            }}
            role="listbox"
            aria-label="Date range presets"
          >
            {PRESETS.map((preset, index) => {
              const isActive = matchedPreset === preset.id;
              const isDark = theme.palette.mode === 'dark';
              const activeBg = isDark ? tokens.accentSoftDark : tokens.accentSoft;
              const activeFg = isDark ? '#A5B4FC' : tokens.accent;
              const idleHoverBg = isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)';
              const hoverBg = isActive ? activeBg : idleHoverBg;
              const showDivider = index === 4; // after Last 30 days, before This week
              return (
                <Box key={preset.id}>
                  {showDivider && (
                    <Box sx={{ borderTop: `1px solid ${theme.palette.divider}`, my: 0.5 }} />
                  )}
                  <ButtonBase
                    role="option"
                    aria-selected={isActive}
                    onClick={() => handlePresetClick(preset.id)}
                    sx={{
                      width: '100%',
                      justifyContent: 'flex-start',
                      padding: '7px 10px',
                      borderRadius: `${tokens.radiusSm}px`,
                      fontSize: 12.5,
                      fontWeight: isActive ? 500 : 400,
                      textAlign: 'left',
                      color: isActive ? activeFg : theme.palette.text.primary,
                      backgroundColor: isActive ? activeBg : 'transparent',
                      '&:hover': { backgroundColor: hoverBg },
                    }}
                  >
                    {preset.label}
                  </ButtonBase>
                </Box>
              );
            })}
          </Stack>
          <Stack sx={{ flex: 1, padding: 1.5 }}>
            <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
              <IconButton size="small" onClick={goPrevMonth} aria-label="Previous month">
                <ChevronLeftIcon fontSize="small" />
              </IconButton>
              <Typography variant="subtitle2" sx={{ fontWeight: 500 }}>
                {monthHeader}
              </Typography>
              <IconButton size="small" onClick={goNextMonth} aria-label="Next month">
                <ChevronRightIcon fontSize="small" />
              </IconButton>
            </Stack>
            <Box
              role="grid"
              aria-label="Calendar"
              sx={{
                display: 'grid',
                gridTemplateColumns: 'repeat(7, 1fr)',
                rowGap: '2px',
              }}
            >
              {WEEKDAYS.map(label => (
                <Box
                  key={label}
                  role="columnheader"
                  sx={{
                    textAlign: 'center',
                    fontSize: 10.5,
                    color: theme.palette.text.secondary,
                    fontWeight: 600,
                    letterSpacing: 0.4,
                    paddingY: 0.5,
                  }}
                >
                  {label.slice(0, 1)}
                </Box>
              ))}
              {grid.map(cell => {
                const cellSeconds = Math.floor(startOfDay(cell.date, timezone).getTime() / 1000);
                const isStart = draftStartDate ? isSameDay(cell.date, draftStartDate, timezone) : false;
                const isEnd = draftEndDate ? isSameDay(cell.date, draftEndDate, timezone) : false;
                const inRange =
                  draft.start !== null &&
                  draft.end !== null &&
                  cellSeconds >= draft.start &&
                  cellSeconds <= draft.end;

                return (
                  <CalendarCell
                    key={cell.date.toISOString()}
                    label={String(ymd(cell.date, timezone).day)}
                    isInMonth={cell.inMonth}
                    isToday={cell.isToday}
                    isStart={isStart}
                    isEnd={isEnd}
                    isInRange={inRange}
                    onClick={() => handleDayClick(cell.date)}
                  />
                );
              })}
            </Box>
          </Stack>
        </Stack>
        <Stack
          direction="row"
          alignItems="center"
          justifyContent="space-between"
          sx={{
            padding: 1.5,
            borderTop: `1px solid ${theme.palette.divider}`,
            backgroundColor:
              theme.palette.mode === 'dark' ? tokens.surface2Dark : tokens.surface2,
          }}
        >
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            {timezone === 'utc' ? 'UTC' : 'Local'}
            {spanLabel}
          </Typography>
          <Stack direction="row" spacing={1}>
            <Button onClick={handleClose} size="small">
              Cancel
            </Button>
            <Button
              variant="contained"
              size="small"
              disabled={!canApply}
              onClick={() => {
                onApply(draft);
                handleClose();
              }}
            >
              Apply
            </Button>
          </Stack>
        </Stack>
      </Popover>
    </>
  );
};

interface CalendarCellProps {
  readonly label: string;
  readonly isInMonth: boolean;
  readonly isToday: boolean;
  readonly isStart: boolean;
  readonly isEnd: boolean;
  readonly isInRange: boolean;
  readonly onClick: () => void;
}

const CalendarCell = ({ label, isInMonth, isToday, isStart, isEnd, isInRange, onClick }: CalendarCellProps) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const isEndpoint = isStart || isEnd;
  const middleOfRange = isInRange && !isEndpoint;

  const rangeBg = isDark ? tokens.accentSoftDark : tokens.accentSoft;

  let background = 'transparent';
  if (isEndpoint) {
    background = tokens.accent;
  } else if (middleOfRange) {
    background = rangeBg;
  }

  // In dark mode the in-range strip is a tinted accent, so primary text on it
  // already reads well; out-of-month cells need a lift from the default
  // text.disabled (slate-500) which gets lost on the dark surface.
  const outOfMonthColor = isDark ? '#94A3B8' : theme.palette.text.disabled;
  let color = isInMonth ? theme.palette.text.primary : outOfMonthColor;
  if (isEndpoint) color = '#FFFFFF';

  let borderRadius = '6px';
  if (isStart && !isEnd) borderRadius = '6px 0 0 6px';
  else if (isEnd && !isStart) borderRadius = '0 6px 6px 0';
  else if (middleOfRange) borderRadius = '0';

  return (
    <ButtonBase
      role="gridcell"
      aria-selected={isEndpoint}
      onClick={onClick}
      sx={{
        height: 30,
        fontSize: 11.5,
        fontFamily: tokens.fontMono,
        backgroundColor: background,
        color,
        borderRadius,
        position: 'relative',
        transition: 'background-color 120ms ease',
        '&:hover': {
          backgroundColor: isEndpoint ? tokens.accent : 'rgba(91, 124, 250, 0.16)',
        },
      }}
    >
      {label}
      {isToday && (
        <Box
          aria-hidden
          sx={{
            position: 'absolute',
            bottom: 4,
            left: '50%',
            transform: 'translateX(-50%)',
            width: 3,
            height: 3,
            borderRadius: '50%',
            backgroundColor: isEndpoint ? '#FFFFFF' : tokens.accent,
          }}
        />
      )}
    </ButtonBase>
  );
};
