---
name: Bug Report
about: Report a bug in mcpx
title: ''
labels: bug
assignees: ''
---

**What happened?**
A clear description of the bug.

**What did you expect?**
What should have happened instead.

**How to reproduce**
Steps to reproduce:
1. Config used (sanitize secrets):
```yaml
# paste mcpx.yaml here
```
2. Command run:
```
mcpx -c mcpx.yaml
```
3. Request that triggered the bug:
```
curl -X POST http://localhost:8080/mcp/server-name ...
```

**Environment**
- mcpx version: 
- OS: 
- Go version: 
- MCP client: (Claude Desktop, Cursor, custom, etc.)
- Backend MCP server: 

**Logs**
```
Paste relevant audit/error logs here
```
