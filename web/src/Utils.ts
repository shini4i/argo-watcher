export const relativeTime = (oldTimestamp: number): string => {
  const timestamp = Date.now();
  const difference = Math.round(timestamp / 1000 - oldTimestamp / 1000);
  if (oldTimestamp === 0) {
    return '-';
  }
  return relativeHumanDuration(difference) + ' ago';
};

export const relativeHumanDuration = (seconds: number): string => {
  function numberEnding(number: number): string {
    return number > 1 ? 's' : '';
  }

  if (seconds < 60) {
    // Less than a minute has passed:
    return `< 1 minute`;
  } else if (seconds < 3600) {
    // Less than an hour has passed:
    const minutes = Math.floor(seconds / 60);
    return `${minutes} minute${numberEnding(minutes)}`;
  } else if (seconds < 86400) {
    // Less than a day has passed:
    const hours = Math.floor(seconds / 3600);
    return `${hours} hour${numberEnding(hours)}`;
  } else if (seconds < 2620800) {
    // Less than a month has passed:
    const days = Math.floor(seconds / 86400);
    return `${days} day${numberEnding(days)}`;
  } else if (seconds < 31449600) {
    // Less than a year has passed:
    const months = Math.floor(seconds / 2620800);
    return `${months} month${numberEnding(months)}`;
  }

  // More than a year has passed:
  return `${Math.floor(seconds / 31449600)} years`;
};

export const relativeTimestamp = (timeframe: number): number => {
  return Math.floor(Date.now() / 1000) - timeframe;
};
