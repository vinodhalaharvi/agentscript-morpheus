// Connect to GitHub MCP server (will prompt for OAuth if needed)
mcp_connect "github" "npx -y @modelcontextprotocol/server-github"

// Create the repository first
>=> mcp "github:create_repository" '{"name": "hello-vite", "description": "Hello World Vite + React app created by AgentScript", "private": false}'

// Create all files in parallel via fan-out
>=> ( mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "package.json",
    "message": "Add package.json",
    "content": "{\"name\": \"hello-vite\", \"private\": true, \"version\": \"0.0.0\", \"type\": \"module\", \"scripts\": {\"dev\": \"vite\", \"build\": \"vite build\", \"preview\": \"vite preview\"}, \"dependencies\": {\"react\": \"^18.2.0\", \"react-dom\": \"^18.2.0\"}, \"devDependencies\": {\"@vitejs/plugin-react\": \"^4.0.0\", \"vite\": \"^5.0.0\"}}"
  }'
  <*> mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "vite.config.js",
    "message": "Add vite config",
    "content": "import { defineConfig } from \"vite\"\nimport react from \"@vitejs/plugin-react\"\n\nexport default defineConfig({\n  plugins: [react()],\n})"
  }'
  <*> mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "index.html",
    "message": "Add index.html",
    "content": "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n  <meta charset=\"UTF-8\" />\n  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\" />\n  <title>Hello Vite + React</title>\n</head>\n<body>\n  <div id=\"root\"></div>\n  <script type=\"module\" src=\"/src/main.jsx\"></script>\n</body>\n</html>"
  }'
  <*> mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "src/main.jsx",
    "message": "Add main.jsx",
    "content": "import React from \"react\"\nimport ReactDOM from \"react-dom/client\"\nimport App from \"./App.jsx\"\n\nReactDOM.createRoot(document.getElementById(\"root\")).render(\n  <React.StrictMode>\n    <App />\n  </React.StrictMode>,\n)"
  }'
  <*> mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "src/App.jsx",
    "message": "Add App.jsx",
    "content": "import { useState } from \"react\"\n\nfunction App() {\n  const [count, setCount] = useState(0)\n\n  return (\n    <div style={{ textAlign: \"center\", marginTop: \"50px\" }}>\n      <h1>Hello from AgentScript!</h1>\n      <p>Vite + React project created via MCP</p>\n      <button onClick={() => setCount(count + 1)}>\n        Count is {count}\n      </button>\n    </div>\n  )\n}\n\nexport default App"
  }'
  <*> mcp "github:create_or_update_file" '{
    "owner": "vinodhalaharvi",
    "repo": "hello-vite",
    "path": "README.md",
    "message": "Add README",
    "content": "# Hello Vite\n\nCreated by AgentScript using GitHub MCP server.\n\n## Run locally\n\n```bash\nnpm install\nnpm run dev\n```\n\nOpen http://localhost:5173"
  }'
)
>=> merge
>=> ask "Summarize what was created"
>=> email "vinod.halaharvi@gmail.com"
