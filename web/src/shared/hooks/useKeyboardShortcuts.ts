import { useEffect, useRef } from 'react';
import { getBrowserDocument } from '../utils/browser';

export type ShortcutHandlers = Readonly<Record<string, () => void>>;

/** True when the event target is somewhere the key is text, not a command. */
const isTypingTarget = (target: EventTarget | null): boolean => {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  // The attribute is checked alongside the property because jsdom does not
  // implement isContentEditable, which would leave the guard untested.
  const editable = target.getAttribute('contenteditable');
  if (target.isContentEditable || (editable !== null && editable !== 'false')) {
    return true;
  }
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
};

/**
 * @description Binds single-key shortcuts on the document, keyed by
 * `event.key`. A key pressed inside an input, or with a modifier held, is left
 * alone — otherwise the shortcut would eat the character the user is typing.
 * @param handlers key to callback; re-reading them per event keeps the listener stable
 */
export const useKeyboardShortcuts = (handlers: ShortcutHandlers): void => {
  const handlersRef = useRef(handlers);
  useEffect(() => {
    handlersRef.current = handlers;
  });

  useEffect(() => {
    const doc = getBrowserDocument();
    if (!doc) {
      return undefined;
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey || isTypingTarget(event.target)) {
        return;
      }
      const handler = handlersRef.current[event.key];
      if (!handler) {
        return;
      }
      event.preventDefault();
      handler();
    };

    doc.addEventListener('keydown', onKeyDown);
    return () => doc.removeEventListener('keydown', onKeyDown);
  }, []);
};
