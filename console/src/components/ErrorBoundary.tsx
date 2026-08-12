import { Component, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

// 全局错误边界：捕获路由/页面渲染异常，避免单个页面崩溃导致整个 Console 白屏。
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  private handleReload = () => {
    this.setState({ error: null });
    window.location.reload();
  };

  render() {
    if (this.state.error) {
      return (
        <div className="flex min-h-screen items-center justify-center p-6">
          <div className="max-w-md space-y-4 text-center">
            <p className="text-lg font-semibold">页面渲染出错</p>
            <p className="text-xs text-destructive break-all">
              {this.state.error.message}
            </p>
            <Button onClick={this.handleReload}>刷新页面</Button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
