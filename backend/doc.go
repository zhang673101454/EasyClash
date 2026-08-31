// Package backend 提供 EasyClash 的核心控制逻辑：
//
//   - proxy_manager.go：mihomo 进程的启动、停止与状态检查
//   - system_proxy.go：跨平台系统全局代理开关（runtime.GOOS）
//   - mihomo_api.go：节点列表、测速与切换的 HTTP 封装
package backend
