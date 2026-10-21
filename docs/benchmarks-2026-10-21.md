# 基准测试结果（2026-10-21）

环境：Apple M1 Pro（darwin/arm64）、Go 1.27.1、`-benchtime=300ms`（MITM）或 `200ms`（解析/匹配）。
运行方式：

```console
$ make bench
```

## 配置与解析

| 基准 | 耗时 | 内存 | 分配 |
|---|---:|---:|---:|
| `StoreLookupHit`（api.github.com） | 17.54 ns/op | 0 B/op | 0 allocs/op |
| `StoreLookupMiss`（example.org） | 14.21 ns/op | 0 B/op | 0 allocs/op |
| `PatternMatch`（`*.*.azure.com`） | 118.5 ns/op | 0 B/op | 0 allocs/op |
| `ParseAnswer`（github.com A） | 339.1 ns/op | 309 B/op | 12 allocs/op |
| `BuildQuery`（api.github.com A） | 198.9 ns/op | 184 B/op | 6 allocs/op |

结论：Store 命中/未命中都在缓存路径上，开销可忽略；域名匹配是正则，约 120ns，远低于一次 TCP 建连。解析器的分配主要来自 IP 切片与域名拆分，属于每域名一次的热路径。

## MITM 端到端（h1 vs h2）

链路：客户端 → MITM（本地 CA）→ 反向代理 → 本地上游，上游返回 5 字节 body，回环网络。

| 基准 | 耗时 | 内存 | 分配 |
|---|---:|---:|---:|
| `MITMHTTP1`（串行） | 120.0 µs/op | 46.6 KB/op | 137 allocs/op |
| `MITMHTTP2`（串行） | 177.0 µs/op | 97.1 KB/op | 154 allocs/op |
| `MITMHTTP1Parallel`（8 并发） | 57.5 µs/op | 48.8 KB/op | 139 allocs/op |
| `MITMHTTP2Parallel`（8 并发） | 51.6 µs/op | 61.2 KB/op | 151 allocs/op |

结论：

- **串行单请求 h2 更慢**（约 +47%）：HPACK、流控和额外的 goroutine/缓冲区是固定开销，小 body 摊不薄。
- **并发时 h2 更快**（约 -10%）：一条连接多路复用，避免了 h1 的多连接建连与队头阻塞。
- h2 的内存分配更高（串行约 2 倍），主要来自每个流的缓冲区与头部编码。

这些数字是回环微基准，不代表真实 GitHub 的收益；真实场景的价值在“页面并发几十个资源、跨网络 RTT 高”，应在浏览器 DevTools 的瀑布图/Protocol 列做最终对比。若后续要优化，优先看 h2 的每流分配与缓冲区复用。
