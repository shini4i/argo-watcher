import { createContext, useContext, useEffect, useMemo, useRef, useState } from 'react';
import PropTypes from 'prop-types';

const context = createContext(null);

export const ErrorProvider = ({ children }) => {
  const [stack, setStack] = useState({});
  const timeouts = useRef([]);

  useEffect(() => {
    return () => timeouts.current.forEach(timeout => clearTimeout(timeout));
  }, []);

  const removeStackItem = (id) => {
    setStack(stack => {
      delete stack[id];
      return { ...stack };
    });
  };

  const setSuccessTimeout = (id) => {
    timeouts.current.push(
      setTimeout(() => removeStackItem(id), 5000)
    );
  };

  const value = useMemo(() => ({
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
      if (!message) {
        message = 'Unknown error';
      }
      setStack(stack => {
        stack[id] = { status: 'error', message };
        return { ...stack };
      });
    },
    setSuccess: (id, message) => {
      setStack(stack => {
        if (!stack[id]) {
          return stack;
        }
        stack[id] = { status: 'success', message };
        setSuccessTimeout(id);
        return { ...stack };
      });
    },
  }), [stack]);

  return (
    <context.Provider value={value}>
      {children}
    </context.Provider>
  );
};

export const useErrorContext = () => {
  return useContext(context);
};

ErrorProvider.propTypes = {
  children: PropTypes.node.isRequired,
};
