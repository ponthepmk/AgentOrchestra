# วิธีต่อ `ao` เข้ากับแต่ละ Agent

หลัง `make build` จะได้ binary ที่ `bin/ao` — เอาไปวางใน `PATH` (เช่น `cp bin/ao /usr/local/bin/ao`)
แล้วตั้งค่าให้แต่ละ agent รู้จัก MCP server นี้

## Claude Code

เพิ่มใน `.mcp.json` ที่ root ของโปรเจกต์ที่จะให้ Claude Code ทำงานด้วย:

```json
{
  "mcpServers": {
    "agentorchestra": {
      "command": "ao",
      "args": ["mcp"]
    }
  }
}
```

หรือใช้คำสั่ง:

```bash
claude mcp add agentorchestra -- ao mcp
```

## Codex CLI

เพิ่มใน `~/.codex/config.toml`:

```toml
[mcp_servers.agentorchestra]
command = "ao"
args = ["mcp"]
```

## Antigravity IDE

เพิ่ม MCP server ในการตั้งค่า (Settings → MCP Servers) แบบ stdio:

- Command: `ao`
- Args: `mcp`

(รูปแบบ config อาจต่างกันตามเวอร์ชัน Antigravity — ดูเอกสาร MCP ของ Antigravity IDE ประกอบ)

## หลังต่อเสร็จ

บอก agent ผ่าน custom instruction / system prompt ของตัวเอง เช่นใน `CLAUDE.md`:

```markdown
โปรเจกต์นี้ใช้ AgentOrchestra ประสานงานกับ agent อื่น:
- เริ่มงานทุกครั้ง: เรียก `ao_status` ก่อน เพื่อดูว่าตอนนี้ถึงขั้นไหน ใครถือไม้อยู่
- จบงานทุกครั้ง: เรียก `ao_handoff` ระบุ target_agent, task ที่ต้องทำต่อ, และ artifacts ที่เกี่ยวข้อง
```

ทำแบบเดียวกันในไฟล์ instruction ของ agent อื่น (เช่น system prompt ของ Antigravity, Codex) โดยเปลี่ยน
เฉพาะชื่อ agent — ทุกตัวคุยผ่าน `.ao/` ไฟล์เดียวกัน ไม่ว่าจะเป็น agent ค่ายไหน
