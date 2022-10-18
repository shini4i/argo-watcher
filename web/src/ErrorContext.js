import { createContext, useContext, useEffect, useRef, useState } from 'react';

const context = createContext(null);

export const ErrorProvider = ({ children }) => {
  const [stack, setStack] = useState({});
  const timeouts = useRef([]);

  useEffect(() => {
    return () => timeouts.current.forEach(timeout => clearTimeout(timeout));
  }, []);

  return (
    <context.Provider
      value={{
        stack,
        messages: Object.keys(stack).reduce((result, key) => {
          let item = stack[key];
          let found = false;
          for (let searchItem of result) {
            if (
              searchItem.message === item.message &&
              searchItem.status === item.status
            ) {
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
          setStack(stack => {
            stack[id] = { status: 'error', message };
            return { ...stack };
          });
        },
        setSuccess: (id, message) => {
          setStack(stack => {
            if (!stack[id]) {
              // don't show success message when there wasn't any error
              return stack;
            }
            // add success message
            stack[id] = { status: 'success', message };
            // set timer to remove
            timeouts.current.push(
              setTimeout(() => {
                setStack(stack => {
                  delete stack[id];
                  return { ...stack };
                });
              }, 5000),
            );
            // return new stack
            return { ...stack };
          });
        },
        clearMessage: (status, message) => {
          setStack(stack => {
            return Object.keys(stack).reduce((result, key) => {
              let item = stack[key];
              if (item.status === status && item.message === message) {
                return result;
              }
              result[key] = item;
              return result;
            }, {});
          });
        },
      }}
    >
      {children}
    </context.Provider>
  );
};

export const useErrorContext = () => {
  return useContext(context);
};
