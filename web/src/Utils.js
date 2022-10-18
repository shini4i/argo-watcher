export const relativeTime = oldTimestamp => {
  const timestamp = Date.now();
  const difference = Math.round(timestamp / 1000 - oldTimestamp / 1000);
  if (oldTimestamp === 0) {
    return '-';
  }
  return relativeHumanDuration(difference) + ' ago';
};

export const relativeHumanDuration = seconds => {
  if (seconds < 60) {
    // Less than a minute has passed:
    return `< 1 minute`;
  } else if (seconds < 3600) {
    // Less than an hour has passed:
    return `${Math.floor(seconds / 60)} minutes`;
  } else if (seconds < 86400) {
    // Less than a day has passed:
    return `${Math.floor(seconds / 3600)} hours`;
  } else if (seconds < 2620800) {
    // Less than a month has passed:
    return `${Math.floor(seconds / 86400)} days`;
  } else if (seconds < 31449600) {
    // Less than a year has passed:
    return `${Math.floor(seconds / 2620800)} months`;
  }

  // More than a year has passed:
  return `${Math.floor(seconds / 31449600)} years`;
};

export const relativeTimestamp = timeframe => {
  return Math.floor(Date.now() / 1000) - timeframe;
};
