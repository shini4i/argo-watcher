declare module 'react-datepicker' {
  import * as React from 'react';
  import { Locale } from 'date-fns';

  interface ReactDatePickerProps {
    selected?: Date | null;
    onChange?: (date: Date | [Date | null, Date | null], event: React.SyntheticEvent<any> | undefined) => void;
    onSelect?: (date: Date, event: React.SyntheticEvent<any> | undefined) => void;
    onClickOutside?: (event: React.SyntheticEvent<any>) => void;
    onChangeRaw?: (event: React.SyntheticEvent<any>) => void;
    onFocus?: (event: React.SyntheticEvent<any>) => void;
    onBlur?: (event: React.SyntheticEvent<any>) => void;
    onKeyDown?: (event: React.SyntheticEvent<any>) => void;
    dateFormat?: string | string[];
    dateFormatCalendar?: string;
    className?: string;
    wrapperClassName?: string;
    calendarClassName?: string;
    todayButton?: React.ReactNode;
    customInput?: React.ReactNode;
    customInputRef?: string;
    placeholderText?: string;
    id?: string;
    name?: string;
    autoComplete?: string;
    disabled?: boolean;
    disabledKeyboardNavigation?: boolean;
    open?: boolean;
    openToDate?: Date;
    minDate?: Date;
    maxDate?: Date;
    selectsStart?: boolean;
    selectsEnd?: boolean;
    startDate?: Date;
    endDate?: Date;
    excludeDates?: Date[];
    filterDate?: (date: Date) => boolean;
    fixedHeight?: boolean;
    formatWeekNumber?: (date: Date) => string | number;
    highlightDates?: Date[] | { [className: string]: Date[] };
    includeDates?: Date[];
    includeTimes?: Date[];
    injectTimes?: Date[];
    inline?: boolean;
    locale?: string | Locale;
    peekNextMonth?: boolean;
    showMonthDropdown?: boolean;
    showPreviousMonths?: boolean;
    showYearDropdown?: boolean;
    dropdownMode?: 'scroll' | 'select';
    timeCaption?: string;
    timeFormat?: string;
    timeIntervals?: number;
    minTime?: Date;
    maxTime?: Date;
    excludeTimes?: Date[];
    useWeekdaysShort?: boolean;
    showTimeSelect?: boolean;
    showTimeSelectOnly?: boolean;
    utcOffset?: number;
    weekLabel?: string;
    withPortal?: boolean;
    showWeekNumbers?: boolean;
    forceShowMonthNavigation?: boolean;
    showDisabledMonthNavigation?: boolean;
    scrollableYearDropdown?: boolean;
    scrollableMonthYearDropdown?: boolean;
    yearDropdownItemNumber?: number;
    previousMonthButtonLabel?: React.ReactNode;
    nextMonthButtonLabel?: React.ReactNode;
    previousYearButtonLabel?: string;
    nextYearButtonLabel?: string;
    timeInputLabel?: string;
    inlineFocusSelectedMonth?: boolean;
    shouldCloseOnSelect?: boolean;
    useShortMonthInDropdown?: boolean;
  }

  class ReactDatePicker extends React.Component<ReactDatePickerProps, any> {}

  export default ReactDatePicker;
}
