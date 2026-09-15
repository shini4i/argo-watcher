import { useCallback } from 'react';
import { useNotify } from 'react-admin';

/**
 * @description Copies text and confirms it with the app's toast. A blocked or
 * missing Clipboard API is reported rather than swallowed, so the user knows to
 * select the text by hand instead of assuming the copy worked.
 * @returns a `copy(text, label)` callback
 */
export const useCopyToClipboard = () => {
  const notify = useNotify();

  return useCallback(
    async (text: string, label = 'Text') => {
      if (!text) {
        return;
      }

      try {
        await navigator.clipboard.writeText(text);
        notify(`${label} copied`, { type: 'info' });
      } catch {
        notify(`${label} could not be copied — select it and copy manually`, { type: 'warning' });
      }
    },
    [notify],
  );
};
