import React, { createContext, useContext, useEffect, useMemo, useRef, useState, ReactNode } from 'react';

export interface ErrorItem {
  status: 'error' | 'success';
  message: string;
}

export interface ErrorContextType {
  stack: Record<string, ErrorItem>;
  messages: ErrorItem[];
  setError: (id: string, message?: string) => void;
  setSuccess: (id: string, message: string) => void;
  clearMessage: (status: 'error' | 'success', message: string) => void;
}

export const ErrorContext = createContext<ErrorContextType | undefined>(undefined);

export const useErrorContext = (): ErrorContextType => {
  const context = useContext(ErrorContext);
  if (!context) {
    throw new Error('useErrorContext must be used within an ErrorProvider');
  }
  return context;
};

interface ErrorProviderProps {
  readonly children: ReactNode;
}

/**
 * Supplies error and success messaging helpers via React context to descendant components.
 *
 * @param props Provider properties containing the wrapped children.
 * @returns The context provider element.
 */
export const ErrorProvider: React.FC<ErrorProviderProps> = ({ children }) => {
  const [stack, setStack] = useState<Record<string, ErrorItem>>({});
  const timeouts = useRef<NodeJS.Timeout[]>([]);

  useEffect(() => {
    return () => {
      for (const timeout of timeouts.current) {
        clearTimeout(timeout);
      }
    };
  }, []);

  const removeStackItem = (id: string) => {
    setStack(stack => {
      const newStack = { ...stack };
      delete newStack[id];
      return newStack;
    });
  };

  const setSuccessTimeout = (id: string) => {
    timeouts.current.push(
      setTimeout(() => removeStackItem(id), 5000)
    );
  };

  /**
   * Clears the first stack entry that matches the provided status/message combination.
   *
   * @param status Message severity to match.
   * @param message Message body to match.
   */
  const clearMessage = (status: 'error' | 'success', message: string) => {
    setStack(stack => {
      const newStack = { ...stack };
      for (const id in newStack) {
        if (newStack[id].status === status && newStack[id].message === message) {
          delete newStack[id];
        }
      }
      return newStack;
    });
  };

  const value = useMemo(() => ({
    stack,
    messages: Object.keys(stack).reduce((result, key) => {
      const item = stack[key];
      const found = result.some(searchItem => searchItem.message === item.message && searchItem.status === item.status);
      if (!found) {
        result.push(item);
      }
      return result;
    }, [] as ErrorItem[]),
    setError: (id: string, message: string = 'Unknown error') => {
      setStack(stack => ({
        ...stack,
        [id]: { status: 'error', message },
      }));
    },
    setSuccess: (id: string, message: string) => {
      setStack(stack => {
        if (!stack[id]) {
          return stack;
        }
        return {
          ...stack,
          [id]: { status: 'success', message },
        };
      });
      setSuccessTimeout(id);
    },
    clearMessage,
  }), [stack]);

  return (
    <ErrorContext.Provider value={value}>
      {children}
    </ErrorContext.Provider>
  );
};
