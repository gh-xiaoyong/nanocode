# 编码约定

仓库内文本文件统一使用：

- 编码：`UTF-8`
- 换行：`LF`

## 目的

这样做是为了避免：

- 中文注释在不同终端里显示乱码
- Markdown 文档在编辑器和命令行里表现不一致
- 不同系统提交时混入多种换行格式

## PowerShell 查看建议

如果 PowerShell 里看到中文乱码，可以先切换输出编码：

```powershell
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Get-Content -Encoding utf8 README.md
```

## 维护约定

1. 新增中文注释或文档时，统一保存为 UTF-8。
2. 不混用本地 ANSI / GBK / UTF-8 with BOM。
3. 如果发现乱码，优先确认文件真实编码，再判断是否需要修复内容。
