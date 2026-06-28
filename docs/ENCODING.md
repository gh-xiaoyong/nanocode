# 编码约定

仓库内文本文件统一使用：

- 编码：`UTF-8`
- 换行：`LF`

## 目的

这样做是为了避免下面这些问题：

- 中文注释在不同终端里显示乱码
- Markdown 文档在编辑器和命令行里表现不一致
- 不同系统提交时混入多种换行格式

## 当前说明

如果你在 PowerShell 里直接 `Get-Content` 某些文件时看到乱码，通常不是文件内容坏了，而是终端输出编码没有切到 UTF-8。

在当前环境里，可以这样查看 UTF-8 文件：

```powershell
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Get-Content -Encoding utf8 README.md
```

## 维护约定

1. 新增含中文的源码注释或文档时，统一保存为 UTF-8
2. 不再混用本地 ANSI / GBK / UTF-8 with BOM
3. 如果发现终端显示乱码，先确认查看方式，再判断是否需要改文件
