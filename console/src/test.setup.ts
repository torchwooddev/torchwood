// vitest 全局 setup：启用 React act 环境（组件测试里手动 act() 包裹
// 服务端帧 / close 事件需要）。
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
