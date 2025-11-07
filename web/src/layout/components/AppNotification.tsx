import * as React from 'react';
import { useState, useEffect, useCallback } from 'react';
import { Alert, AlertTitle, Button, Snackbar, type SnackbarProps, type AlertColor } from '@mui/material';
import type { SxProps, Theme } from '@mui/material/styles';
import type { NotificationProps } from 'react-admin';
import {
  CloseNotificationContext,
  type NotificationPayload,
  undoableEventEmitter,
  useNotificationContext,
  useTakeUndoableMutation,
  useTranslate,
} from 'ra-core';

const NETWORK_ERROR_MESSAGE = 'Network error';
const NETWORK_ERROR_HELP =
  'Unable to reach the Argo Watcher API. Ensure the backend container is running and reachable at VITE_API_BASE_URL (for example http://backend:8080 in docker-compose or http://localhost:8080 locally).';

const defaultAnchorOrigin: SnackbarProps['anchorOrigin'] = {
  vertical: 'bottom',
  horizontal: 'center',
};

const defaultSnackbarOffset: SxProps<Theme> = theme => ({
  '&.MuiSnackbar-root': {
    bottom: `calc(${theme.spacing(12)} + env(safe-area-inset-bottom))`,
  },
});

/** Splits the MUI sx prop from the rest of the props helper components forward. */
const extractSx = <T extends object>(input: T): [SxProps<Theme> | undefined, Omit<T, 'sx'>] => {
  const { sx, ...rest } = input as T & { sx?: SxProps<Theme> };
  return [sx, rest];
};

/** Maps react-admin notification types to MUI alert severities. */
const severityFromType = (type?: string): AlertColor => {
  switch (type) {
    case 'error':
    case 'warning':
    case 'info':
    case 'success':
      return type;
    default:
      return 'info';
  }
};

/**
 * Custom notification component that mirrors react-admin notifications while supporting undo flows.
 */
export const AppNotification = (props: NotificationProps) => {
  const { autoHideDuration = 4000, anchorOrigin = defaultAnchorOrigin, className, type = 'info', ...rest } = props;
  const { notifications, takeNotification } = useNotificationContext();
  const takeMutation = useTakeUndoableMutation();
  const translate = useTranslate();
  const [open, setOpen] = useState(false);
  const [currentNotification, setCurrentNotification] = useState<
    NotificationPayload | undefined
  >(undefined);

  useEffect(() => {
    if (notifications.length && !currentNotification) {
      const notification = takeNotification();
      if (notification) {
        setCurrentNotification(notification);
        setOpen(true);
      }
    }

    if (currentNotification?.notificationOptions?.undoable) {
      const beforeUnload = (event: BeforeUnloadEvent) => {
        event.preventDefault();
        const confirmationMessage = '';
        event.returnValue = confirmationMessage;
        return confirmationMessage;
      };

      window.addEventListener('beforeunload', beforeUnload);
      return () => {
        window.removeEventListener('beforeunload', beforeUnload);
      };
    }

    return undefined;
  }, [notifications, currentNotification, takeNotification]);

  /** Closes the snackbar without triggering undo behavior. */
  const handleClose = useCallback(() => {
    setOpen(false);
  }, []);

  /** Invoked after the snackbar exits to flush undoable mutations. */
  const handleExited = useCallback(() => {
    if (currentNotification?.notificationOptions?.undoable) {
      const mutation = takeMutation();
      if (mutation) {
        mutation({ isUndo: false });
      } else {
        undoableEventEmitter.emit('end', { isUndo: false });
      }
    }
    setCurrentNotification(undefined);
  }, [currentNotification, takeMutation]);

  /** Executes the undo callback when the user presses the Undo button. */
  const handleUndo = useCallback(() => {
    const mutation = takeMutation();
    if (mutation) {
      mutation({ isUndo: true });
    } else {
      undoableEventEmitter.emit('end', { isUndo: true });
    }
    setOpen(false);
  }, [takeMutation]);

  if (!currentNotification) {
    return null;
  }

  const {
    message,
    type: messageType,
    notificationOptions = {},
  } = currentNotification;
  const {
    autoHideDuration: messageAutoHide,
    messageArgs,
    multiLine,
    undoable,
    ...notificationProps
  } = notificationOptions;

  const translatedMessage =
    typeof message === 'string' ? translate(message, messageArgs) : message;

  const isNetworkError = translatedMessage === NETWORK_ERROR_MESSAGE;
  const effectiveMessage =
    isNetworkError && typeof translatedMessage === 'string'
      ? NETWORK_ERROR_HELP
      : translatedMessage;
  const effectiveAutoHide =
    isNetworkError || messageAutoHide === null
      ? null
      : messageAutoHide ?? autoHideDuration;

  const severity = severityFromType(messageType || type);

  const [restSx, restWithoutSx] = extractSx(rest);
  const [notificationSx, notificationWithoutSx] = extractSx(notificationProps);
  const combinedSx = [defaultSnackbarOffset, restSx, notificationSx].filter(Boolean) as SxProps<Theme>[];

  return (
    <CloseNotificationContext.Provider value={handleClose}>
      <Snackbar
        className={className}
        open={open}
        onClose={handleClose}
        TransitionProps={{ onExited: handleExited }}
        anchorOrigin={anchorOrigin}
        autoHideDuration={effectiveAutoHide ?? undefined}
        disableWindowBlurListener={undoable}
        sx={combinedSx}
        {...(restWithoutSx as NotificationProps)}
        {...(notificationWithoutSx as NotificationProps)}
      >
        <Alert
          severity={severity}
          variant="filled"
          onClose={!undoable ? handleClose : undefined}
          sx={{
            minWidth: 320,
            alignItems: 'flex-start',
            whiteSpace: multiLine ? 'pre-wrap' : 'normal',
          }}
          action={
            undoable ? (
              <Button color="inherit" size="small" onClick={handleUndo}>
                {translate('ra.action.undo')}
              </Button>
            ) : undefined
          }
        >
          {isNetworkError ? <AlertTitle>Network error</AlertTitle> : null}
          {typeof effectiveMessage === 'string' ? (
            effectiveMessage
          ) : (
            <div>{effectiveMessage}</div>
          )}
        </Alert>
      </Snackbar>
    </CloseNotificationContext.Provider>
  );
};
