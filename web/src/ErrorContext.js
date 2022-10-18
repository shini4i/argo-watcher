import { createContext, useContext, useState } from 'react';

const context = createContext(null);


export const ErrorProvider = ({ children }) => {
  const [stack, setStack] = useState({});

  return <context.Provider value={{
    stack,
    messages: Object.keys(stack).reduce((result, key) => {
      let item = stack[key];
      let found = false;
      for(let searchItem of result) {
        if (searchItem.message === item.message && searchItem.status === item.status) {
          found = true;
          break;
        }
      }
      if (!found) {
        result.push(item);
      }
      return result;
    }, []),
    setError: (id, message) => {
      setStack((stack) => {
        stack[id] = { status: 'error', message };
        return { ...stack };
      });
    },
    setSuccess: (id, message) => {
      setStack((stack) => {
        if (!stack[id]) {
          // don't show success message when there wasn't any error
          return stack;
        }
        stack[id] = { status: 'success', message };
        return { ...stack };
      });
    },
    clearMessage: (status, message) => {
      setStack((stack) => {
        return Object.keys(stack).reduce((result, key) => {
          let item = stack[key];
          if (item.status === status && item.message === message) {
            return result;
          }
          result[key] = item;
          return result;
        }, {})
      });
    },
  }}>{children}</context.Provider>;
};

export const useErrorContext = () => {
  return useContext(context);
}
