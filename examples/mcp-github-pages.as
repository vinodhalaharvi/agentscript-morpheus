// Connect to GitHub MCP server
mcp_connect "github" "npx -y @modelcontextprotocol/server-github"

// Create repo
>=> mcp "github:create_repository" '{"name": "agentscript-demo", "description": "AgentScript - AI-Powered DSL for Gemini API", "private": false}'

// Add README
mcp "github:create_or_update_file" '{"owner": "vinodhalaharvi", "repo": "agentscript-demo", "path": "README.md", "message": "Initial commit", "content": "# AgentScript Demo", "branch": "main"}'

// Add simple index.html
mcp "github:create_or_update_file" '{"owner": "vinodhalaharvi", "repo": "agentscript-demo", "path": "index.html", "message": "Add landing page", "content": "<!DOCTYPE html><html><head><title>AgentScript</title></head><body style=background:#667eea;color:white;text-align:center;padding:50px;font-family:system-ui><h1>AgentScript</h1><p>AI-Powered DSL for Gemini API</p><pre style=background:rgba(0,0,0,0.3);padding:20px;display:inline-block>search \"AI\" >=> summarize >=> email \"team@co.com\"</pre><p><a href=https://github.com/vinodhalaharvi/agentscript style=color:white>GitHub</a></p></body></html>", "branch": "main"}'

ask "Done! Enable Pages at: https://github.com/vinodhalaharvi/agentscript-demo/settings/pages"
