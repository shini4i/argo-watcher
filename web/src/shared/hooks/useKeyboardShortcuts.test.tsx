import { fireEvent, render } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useKeyboardShortcuts, type ShortcutHandlers } from './useKeyboardShortcuts';

const Probe = ({ handlers }: { handlers: ShortcutHandlers }) => {
  useKeyboardShortcuts(handlers);
  return (
    <>
      <input aria-label="text" />
      <textarea aria-label="area" />
      <div aria-label="editable" contentEditable suppressContentEditableWarning />
      <button type="button">plain</button>
    </>
  );
};

describe('useKeyboardShortcuts', () => {
  let onF: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    onF = vi.fn();
  });

  it('runs the handler for a bare key press', () => {
    render(<Probe handlers={{ f: onF }} />);
    fireEvent.keyDown(document, { key: 'f' });
    expect(onF).toHaveBeenCalledTimes(1);
  });

  it('ignores keys with no handler', () => {
    render(<Probe handlers={{ f: onF }} />);
    fireEvent.keyDown(document, { key: 'q' });
    expect(onF).not.toHaveBeenCalled();
  });

  it.each(['metaKey', 'ctrlKey', 'altKey'] as const)('ignores a press with %s held', modifier => {
    render(<Probe handlers={{ f: onF }} />);
    fireEvent.keyDown(document, { key: 'f', [modifier]: true });
    expect(onF).not.toHaveBeenCalled();
  });

  it.each([
    ['text', 'input'],
    ['area', 'textarea'],
    ['editable', 'contenteditable'],
  ])('leaves the key to the %s field', label => {
    const { getByLabelText } = render(<Probe handlers={{ f: onF }} />);
    fireEvent.keyDown(getByLabelText(label), { key: 'f' });
    expect(onF).not.toHaveBeenCalled();
  });

  it('still fires from a non-typing element such as a button', () => {
    const { getByRole } = render(<Probe handlers={{ f: onF }} />);
    fireEvent.keyDown(getByRole('button', { name: 'plain' }), { key: 'f' });
    expect(onF).toHaveBeenCalledTimes(1);
  });

  it('calls the latest handler after a re-render', () => {
    const replacement = vi.fn();
    const { rerender } = render(<Probe handlers={{ f: onF }} />);

    rerender(<Probe handlers={{ f: replacement }} />);
    fireEvent.keyDown(document, { key: 'f' });

    expect(onF).not.toHaveBeenCalled();
    expect(replacement).toHaveBeenCalledTimes(1);
  });

  it('stops listening once unmounted', () => {
    const { unmount } = render(<Probe handlers={{ f: onF }} />);
    unmount();
    fireEvent.keyDown(document, { key: 'f' });
    expect(onF).not.toHaveBeenCalled();
  });
});
