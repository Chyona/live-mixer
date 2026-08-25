import { Component, Fragment, type ErrorInfo, type ReactNode } from 'react';
import { Button, Result } from 'antd';
import { DEFAULT_APP_PATH } from '~/routes/const';
import { markHmrRecoveryPending } from '~/utils/hmrRecovery';

type Props = { children: ReactNode };
type State = { hasError: boolean; resetKey: number };

class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, resetKey: 0 };

  static getDerivedStateFromError(): Partial<State> {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (import.meta.env.DEV) {
      markHmrRecoveryPending();
    }
    console.error('ErrorBoundary caught:', error, info.componentStack);
  }

  private handleReset = () => {
    this.setState((state) => ({
      hasError: false,
      resetKey: state.resetKey + 1,
    }));
    window.location.assign(DEFAULT_APP_PATH);
  };

  render() {
    if (this.state.hasError) {
      return (
        <Result
          status="error"
          title="页面出错了"
          subTitle="请刷新页面或返回首页后重试。"
          extra={
            <Button type="primary" onClick={this.handleReset}>
              返回首页
            </Button>
          }
        />
      );
    }

    return <Fragment key={this.state.resetKey}>{this.props.children}</Fragment>;
  }
}

export default ErrorBoundary;
