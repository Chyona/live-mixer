import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import ErrorBoundary from './index';

const ThrowError = () => {
  throw new Error('test error');
};

describe('ErrorBoundary', () => {
  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <span>content ok</span>
      </ErrorBoundary>
    );

    expect(screen.getByText('content ok')).toBeInTheDocument();
  });

  it('renders fallback when child throws', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});

    render(
      <ErrorBoundary>
        <ThrowError />
      </ErrorBoundary>
    );

    expect(screen.getByText('页面出错了')).toBeInTheDocument();

    consoleError.mockRestore();
  });
});
