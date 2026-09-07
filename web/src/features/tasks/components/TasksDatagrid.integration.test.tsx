import { render, screen, waitFor, within } from '@testing-library/react';
import { AdminContext, testDataProvider } from 'react-admin';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { TaskListLayout } from './TaskListLayout';
import { TasksDatagrid } from './TasksDatagrid';

const task = (overrides: Partial<Task>): Task => ({
  id: 'task-1',
  app: 'checkout-api',
  author: 'jane@example.com',
  project: 'acme/checkout',
  created: 1_780_000_000,
  updated: 1_780_000_060,
  images: [{ image: 'acme/checkout', tag: 'v1' }],
  status: 'deployed',
  ...overrides,
});

const FAILED = task({
  id: 'failed-task',
  app: 'payments-worker',
  status: 'failed',
  status_reason:
    'Application deployment failed. Rollout status is not available\n\nSync operation phase: Failed\nSync operation message: one or more objects failed to apply',
});

const CANCELLED = task({
  id: 'cancelled-task',
  app: 'cart',
  status: 'cancelled',
  status_reason: 'Cancelled by lee@example.com',
});

const CLEAN = task({ id: 'clean-task', app: 'billing', status: 'deployed' });

/**
 * @description Drives the real react-admin Datagrid, so the DatagridBody `row`
 * extension point and the derived colSpan are exercised rather than a stub. The
 * sibling unit suite mocks react-admin, so only this one can hold those honest.
 */
const renderDatagrid = (records: Task[]) =>
  render(
    <MemoryRouter>
      <AdminContext
        dataProvider={testDataProvider({
          getList: vi.fn(() => Promise.resolve({ data: records, total: records.length })),
        })}
      >
        <TaskListLayout perPageStorageKey="integration.perPage">
          <TasksDatagrid />
        </TaskListLayout>
      </AdminContext>
    </MemoryRouter>,
  );

/** The panel row is the only one rendering a single spanning cell. */
const panelRows = () =>
  screen
    .getAllByRole('row')
    .filter(row => row.querySelectorAll('td').length === 1 && row.querySelector('td[colspan]'));

describe('TasksDatagrid on real react-admin', () => {
  it('renders one reason panel per task carrying a status_reason', async () => {
    renderDatagrid([FAILED, CANCELLED, CLEAN]);

    await waitFor(() => expect(screen.getByText('payments-worker')).toBeInTheDocument());

    expect(panelRows()).toHaveLength(2);
    expect(screen.getByText('Application deployment failed. Rollout status is not available')).toBeInTheDocument();
    expect(screen.getByText('Cancelled by lee@example.com')).toBeInTheDocument();
  });

  it('renders no panel for a task without a status_reason', async () => {
    renderDatagrid([CLEAN]);

    await waitFor(() => expect(screen.getByText('billing')).toBeInTheDocument());
    expect(panelRows()).toHaveLength(0);
  });

  it('places the panel directly beneath its own task row', async () => {
    renderDatagrid([FAILED, CLEAN]);

    await waitFor(() => expect(screen.getByText('payments-worker')).toBeInTheDocument());

    const rows = screen.getAllByRole('row');
    const ownerIndex = rows.findIndex(row => within(row).queryByText('payments-worker'));
    expect(ownerIndex).toBeGreaterThan(-1);
    expect(rows[ownerIndex + 1]).toBe(panelRows()[0]);
  });

  // Queried from the header rather than hardcoded, so the assertion tracks the
  // column set instead of pinning today's count.
  it('spans the panel across every rendered column', async () => {
    renderDatagrid([FAILED]);

    await waitFor(() => expect(screen.getByText('payments-worker')).toBeInTheDocument());

    const headerCells = screen.getAllByRole('columnheader').length;
    expect(headerCells).toBeGreaterThan(1);
    expect(panelRows()[0].querySelector('td')).toHaveAttribute('colspan', String(headerCells));
  });

  it('offers the drill-down link from the panel, pointing at its own task', async () => {
    renderDatagrid([FAILED]);

    await waitFor(() => expect(screen.getByText('payments-worker')).toBeInTheDocument());

    expect(screen.getByRole('link', { name: 'Full reason' })).toHaveAttribute(
      'href',
      '/task/failed-task',
    );
  });

  it('drops the separate project column, carrying the project in the app cell', async () => {
    renderDatagrid([CLEAN]);

    await waitFor(() => expect(screen.getByText('billing')).toBeInTheDocument());

    const headers = screen.getAllByRole('columnheader').map(cell => cell.textContent?.trim());
    expect(headers).toEqual(['Application', 'Status', 'Image · tag', 'Author', 'When', 'Duration']);
    expect(screen.getByText('acme/checkout')).toBeInTheDocument();
  });
});
