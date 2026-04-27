import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  type ReactNode,
} from 'react';

export interface TaskListState {
  readonly pausedReasons: ReadonlySet<string>;
  readonly intervalSec: number;
  readonly lastRefetchedAt: number;
}

type Action =
  | { type: 'pause'; reason: string }
  | { type: 'resume'; reason: string }
  | { type: 'setInterval'; intervalSec: number }
  | { type: 'markRefetched'; at?: number };

const initialState: TaskListState = {
  pausedReasons: new Set<string>(),
  intervalSec: 30,
  lastRefetchedAt: Date.now(),
};

const reducer = (state: TaskListState, action: Action): TaskListState => {
  switch (action.type) {
    case 'pause': {
      if (state.pausedReasons.has(action.reason)) {
        return state;
      }
      const next = new Set(state.pausedReasons);
      next.add(action.reason);
      return { ...state, pausedReasons: next };
    }
    case 'resume': {
      if (!state.pausedReasons.has(action.reason)) {
        return state;
      }
      const next = new Set(state.pausedReasons);
      next.delete(action.reason);
      return { ...state, pausedReasons: next };
    }
    case 'setInterval':
      return { ...state, intervalSec: action.intervalSec, lastRefetchedAt: Date.now() };
    case 'markRefetched':
      return { ...state, lastRefetchedAt: action.at ?? Date.now() };
    default:
      return state;
  }
};

interface TaskListContextValue {
  readonly state: TaskListState;
  readonly pause: (reason: string) => void;
  readonly resume: (reason: string) => void;
  readonly setInterval: (intervalSec: number) => void;
  readonly markRefetched: () => void;
}

const TaskListContext = createContext<TaskListContextValue | undefined>(undefined);

interface TaskListProviderProps {
  readonly children: ReactNode;
  readonly initialIntervalSec?: number;
}

/**
 * Provides shared state for the task list page so the toolbar (timer) and the
 * table body (hover/expand) can coordinate auto-refresh pauses.
 */
export const TaskListProvider = ({ children, initialIntervalSec = 30 }: TaskListProviderProps) => {
  const [state, dispatch] = useReducer(reducer, undefined, () => ({
    ...initialState,
    intervalSec: initialIntervalSec,
  }));

  const pause = useCallback((reason: string) => dispatch({ type: 'pause', reason }), []);
  const resume = useCallback((reason: string) => dispatch({ type: 'resume', reason }), []);
  const setIntervalSec = useCallback(
    (intervalSec: number) => dispatch({ type: 'setInterval', intervalSec }),
    [],
  );
  const markRefetched = useCallback(() => dispatch({ type: 'markRefetched' }), []);

  const value = useMemo(
    () => ({ state, pause, resume, setInterval: setIntervalSec, markRefetched }),
    [state, pause, resume, setIntervalSec, markRefetched],
  );

  return <TaskListContext.Provider value={value}>{children}</TaskListContext.Provider>;
};

const noopValue: TaskListContextValue = {
  state: initialState,
  pause: () => {},
  resume: () => {},
  setInterval: () => {},
  markRefetched: () => {},
};

/** Returns the task-list controller; safe to call without a provider. */
export const useTaskListContext = (): TaskListContextValue => {
  const ctx = useContext(TaskListContext);
  return ctx ?? noopValue;
};

/**
 * Pauses auto-refresh under a named reason for the lifetime of the calling
 * component. Pass `active=false` to opt out conditionally.
 */
export const usePauseRefresh = (reason: string, active = true): void => {
  const { pause, resume } = useTaskListContext();
  useEffect(() => {
    if (!active) {
      return undefined;
    }
    pause(reason);
    return () => resume(reason);
  }, [reason, active, pause, resume]);
};
