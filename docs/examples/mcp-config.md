# วิธีต่อ `ao` เข้ากับแต่ละ Agent

หลัง `make build` จะได้ binary ที่ `bin/ao` — เอาไปวางใน `PATH` (เช่น `cp bin/ao /usr/local/bin/ao`)

> **ทางลัด:** `ao init --id <project>` เจน `.mcp.json`, `CLAUDE.md`, `AGENTS.md` ให้อัตโนมัติ
> และพิมพ์ config ของ Claude Desktop ให้ copy — ส่วนใหญ่ไม่ต้องตั้งอะไรในหน้านี้เองเลย
> หน้านี้เก็บไว้อ้างอิงกรณีต้องตั้งเองหรือใช้ agent ที่ init ไม่ครอบคลุม

## Claude Code

`ao init` สร้าง `.mcp.json` ให้แล้ว ถ้าต้องทำเอง:

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

## OpenCode

อ่าน `.mcp.json` เดียวกับ Claude Code (หรือเพิ่มใน config ของ OpenCode ด้วย command `ao`, args
`["mcp"]` แบบ stdio) และอ่านกติกาจาก `AGENTS.md` ที่ `ao init` เจนให้

## Claude Desktop

Claude Desktop ใช้ config ไฟล์เดียวระดับเครื่อง (ไม่ใช่ต่อโปรเจกต์แบบ `.mcp.json`) — แก้ไฟล์
`claude_desktop_config.json`:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux:** `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "agentorchestra": {
      "command": "/absolute/path/to/ao",
      "args": ["mcp"]
    }
  }
}
```

ต้องใช้ **absolute path** ไปที่ binary เสมอ (ต่างจาก Claude Code CLI ที่หา `ao` ใน `PATH` ให้ได้) แล้ว
**restart Claude Desktop ทั้งแอป** ให้โหลด config ใหม่ (`ao init` พิมพ์ config พร้อม path ที่ถูกต้อง
ให้ copy อยู่แล้ว)

เนื่องจาก config นี้เป็นระดับเครื่อง ไม่ใช่ต่อโปรเจกต์ — ใช้ **project registry** ช่วย: โปรเจกต์ที่
`ao init` แล้วถูกลงทะเบียนไว้ ทำให้ Claude Desktop เรียก tool ด้วย `project_id: "my-project"` สั้นๆ
หรือไม่ระบุเลย (ได้ default project) แทนการพิมพ์ absolute path ทุกครั้ง — ดูรายการโปรเจกต์ด้วย tool
`ao_projects`

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

## LLM local (Ollama / LM Studio / llama.cpp / vLLM)

Model server พวกนี้**ต่อ MCP เองไม่ได้** — ใช้ `ao worker` เป็นตัวขับแทน (ไม่ต้องตั้ง config ในแอปไหน):

```bash
ao worker --agent ollama-worker --url http://localhost:11434/v1 --model qwen2.5:3b   # Ollama
ao worker --agent ollama-worker --url http://localhost:1234/v1  --model <model>     # LM Studio
ao worker --agent ollama-worker --url http://localhost:8080/v1  --model default     # llama.cpp
ao worker --agent ollama-worker --url http://localhost:8000/v1  --model <model>     # vLLM
```

เปิดทิ้งไว้ — งานที่ handoff ถึง `ollama-worker` จะถูกทำและส่งไม้กลับอัตโนมัติ

## หลังต่อเสร็จ

`ao init` เจนกติกาลง `CLAUDE.md` (Claude Code/Desktop) และ `AGENTS.md` (Codex, OpenCode,
Antigravity) ให้แล้ว — ใจความคือ:

```markdown
- เริ่ม session: เรียก `ao_agents` ดูทีม+ความถนัด+ใคร online แล้ว `ao_status` ดูว่างานถึงตาใคร
- จบงานทุกครั้ง: เรียก `ao_handoff` ระบุ target_agent (เลือกตาม capabilities), task, notes, artifacts
- ระบุตัวเองด้วย arg `agent` เพื่อให้เพื่อนร่วมทีมเห็นว่า online
```

ถ้า agent ตัวไหนไม่อ่านทั้งสองไฟล์นี้ ให้ copy ข้อความนี้ไปใส่ system prompt ของมันเอง — ทุกตัวคุยผ่าน
`.ao/` ไฟล์เดียวกัน ไม่ว่าจะเป็น agent ค่ายไหน
