import { render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AppLayout } from './AppLayout';

const { layoutCalls, LayoutMock, AppTopBarStub } = vi.hoisted(() => {
  const layoutCallsInternal: Array<Record<string, unknown>> = [];

  const layout = (props: Record<string, unknown>) => {
    layoutCallsInternal.push(props);
    return <div data-testid="layout">{props.children}</div>;
  };

  const topBar = () => <div data-testid="app-top-bar" />;

  return { layoutCalls: layoutCallsInternal, LayoutMock: layout, AppTopBarStub: topBar };
});

vi.mock('react-admin', () => ({
  Layout: LayoutMock,
}));

vi.mock('./components/AppTopBar', () => ({
  AppTopBar: AppTopBarStub,
}));

describe('AppLayout', () => {
  it('injects the custom top bar, menu, and sidebar shims', () => {
    render(<AppLayout />);

    expect(layoutCalls).toHaveLength(1);
    const props = layoutCalls[0] as {
      appBar: unknown;
      menu: () => unknown;
      sidebar: () => unknown;
      sx: unknown;
    };
    expect(props.appBar).toBe(AppTopBarStub);
    expect(props.menu({} as never)).toBeNull();
    expect(props.sidebar({} as never)).toBeNull();
    expect(typeof props.sx).toBe('function');
  });

  it('applies custom layout styles removing top margin on all breakpoints', () => {
    render(<AppLayout />);
    const props = layoutCalls[0] as { sx: (theme: ThemeLike) => Record<string, unknown> };
    const theme: ThemeLike = {
      breakpoints: {
        down: () => '@media (max-width:600px)',
      },
    };

    const styles = props.sx(theme);
    expect(styles['& .RaLayout-appFrame']).toMatchObject({ marginTop: 0 });
    expect(styles['& .RaLayout-appFrame']['@media (max-width:600px)']).toMatchObject({ marginTop: 0 });
  });
});

interface ThemeLike {
  breakpoints: {
    down: (value: string) => string;
  };
}
