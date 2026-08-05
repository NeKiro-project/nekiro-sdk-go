# NeKiro Go SDK RepoWiki

这里是公共 Go Agent SDK 和应用 SDK 的中英文 RepoWiki 入口。仓库内的
README 是 canonical source，MkDocs 页面由 CI 从这些文档生成。

## 从这里开始

- [源文档](source-docs/index.md)：SDK 总览、Agent SDK 和 Workspace Client。
- [GitHub 仓库](https://github.com/NeKiro-project/nekiro-sdk-go)：源码、Issue 和 Release。
- [Core RepoWiki](https://nekiro-project.github.io/NeKiro/zh/)：平台契约与架构。

SDK 不导入 Core service internal，不负责 endpoint discovery、重试、备用
组件选择，也不提供 Agent Runtime。
