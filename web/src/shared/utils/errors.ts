import { HttpError } from 'react-admin';

export interface NormalizedError {
  message: string;
  status?: number;
  details?: unknown;
}

/** Type guard that checks whether the provided value is a react-admin HttpError. */
const isHttpError = (error: unknown): error is HttpError =>
  typeof error === 'object' && error !== null && 'status' in error && 'message' in error;

/** Normalizes various thrown values into a consistent error payload for the UI. */
export const normalizeError = (error: unknown): NormalizedError => {
  if (isHttpError(error)) {
    return {
      message: error.message,
      status: error.status,
      details: error.body,
    };
  }

  if (error instanceof Error) {
    return {
      message: error.message,
    };
  }

  if (typeof error === 'string') {
    return { message: error };
  }

  return {
    message: 'An unexpected error occurred',
    details: error,
  };
};
