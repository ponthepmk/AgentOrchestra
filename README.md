# AgentOrchestra (AO)

**Multi-Agent Orchestrator** — ตัว "วาทยากร" ที่ส่งต่อ context ระหว่าง AI agent หลายตัว
(Claude Code, Claude Desktop, Codex, Antigravity IDE, OpenCode, LLM local) ในโปรเจกต์เดียวกัน
โดยไม่ต้อง copy-paste เอง

ต่อผ่าน **MCP (Model Context Protocol)** — มาตรฐานกลางที่ agent เกือบทุกค่ายรองรับ — เขียนด้วย **Go**
เป็น binary เดียว รันได้ทั้งบนเครื่องตัวเองและ K3s Home Lab

> 🚀 **เริ่มใช้ใน 3 ขั้น:** [QUICKSTART.md](./QUICKSTART.md) — ติดตั้ง → `ao init` → `ao ui`
> จากนั้นแทบไม่ต้องจำคำสั่งอะไรอีก: พิมพ์ `ao` เฉยๆ ได้หน้าสรุป+คำแนะนำ, `ao ui` ได้ dashboard

## สถาปัตยกรรม

```
[Claude Code]────┐ (MCP / stdio)
[Claude Desktop]─┤
[Antigravity]────┼──> ao (Go binary, MCP server) ──> .ao/ files (source of truth)
[Codex/OpenCode]─┘         │                          ├─> SQLite index (rebuildable)
                           │                          ├─> .ao/presence/ (ใคร online)
[Ollama/LM Studio/  <── ao worker (ตัวขับ LLM local)  └─> HANDOFF.md (mirror อ่านง่าย)
 llama.cpp/vLLM]           │
                           └─ CLI สำหรับมนุษย์: ao init / status / agents / handoff / log / watch
```

- **`.ao/` ในโปรเจกต์เป้าหมาย = source of truth** — เป็นไฟล์ธรรมดา commit เข้า git ได้ ติดไปกับ repo
- **SQLite = index รอง** ที่ rebuild ได้เสมอ (`ao log --reindex`)
- **binary เดียวหลายโหมด:** `ao mcp` (ให้ agent ต่อ), `ao worker` (ขับ LLM local), CLI (คนสั่งเอง) —
  logic ชุดเดียวกัน (`internal/orchestrator`) จึงเห็นสถานะตรงกันเสมอ

เหตุผลการออกแบบทั้งหมด: [`docs/SPEC.md`](./docs/SPEC.md)

## เริ่มใช้งาน — คำสั่งเดียวจบ

```bash
make build && cp bin/ao /usr/local/bin/ao   # ครั้งเดียวต่อเครื่อง

cd your-project
ao init --id my-project        # ← คำสั่งเดียว ได้ครบทุกอย่าง
```

`ao init` ทำให้อัตโนมัติ:
- `.agentconfig` พร้อม**ทีมมาตรฐาน 4 ตัว + ความถนัด** (claude-code, codex, antigravity-ide,
  ollama-worker) — แก้เพิ่ม/ลดได้ตามใจ
- `.mcp.json` — Claude Code / OpenCode เห็น MCP server ทันทีที่เปิดโปรเจกต์
- `CLAUDE.md` + `AGENTS.md` — กติกาสอน agent ให้เรียก `ao_agents`/`ao_status` ก่อนเริ่มงาน และ
  `ao_handoff` เมื่อจบงาน (ไม่ต้อง copy เอง)
- ลงทะเบียนโปรเจกต์ใน registry เครื่อง — คำสั่ง/tool ทุกตัวหลังจากนี้**ไม่ต้องพิมพ์ path อีก**
- พิมพ์ config สำหรับ Claude Desktop ให้ copy ไปวาง (พร้อม absolute path ที่ถูกต้อง)

## ทีมและความถนัด (ใครทำอะไรได้ / ใคร online)

```
$ ao agents
claude-code        🟢 online   [planning, coding, refactoring, review]  — วางแผนและเขียนโค้ด
codex              ⚪ offline  [planning, coding]                       — สลับ/เสริมกับ claude-code
antigravity-ide    🟢 online   [infographic, diagram, documentation]    — ทำภาพ infographic/diagram
ollama-worker      🟢 online   [summarize, small-tasks, translation]    — โมเดลเล็กทำงานย่อยตามสั่ง
```

- agent เรียก tool `ao_agents` ตอน connect → รู้ทันทีว่าทีมมีใคร ถนัดอะไร ใครอยู่ — แล้วเลือกส่งงาน
  ตาม capabilities ได้ถูกตัว
- `ao_handoff` ปฏิเสธ target ที่ไม่อยู่ในทีม (กันส่งงานหาตัวที่ไม่มีจริง)
- Presence: agent ที่ระบุตัวเอง (arg `agent`) หรือส่ง handoff จะขึ้น 🟢 online (หน้าต่าง 10 นาที)

## ใช้ LLM local (Ollama / LM Studio / llama.cpp / vLLM) เป็นลูกทีม

Model server พวกนี้ต่อ MCP เองไม่ได้ — `ao worker` เป็นตัวขับให้: รับงานที่ส่งถึงมัน → ยิง API →
เขียนผลลง `.ao/outputs/` → ส่งไม้กลับให้ผู้ส่งอัตโนมัติ ทุกตัวใช้ OpenAI-compatible API เหมือนกันหมด:

```bash
# Ollama
ao worker --agent ollama-worker --url http://localhost:11434/v1 --model qwen2.5:3b
# LM Studio
ao worker --agent ollama-worker --url http://localhost:1234/v1 --model <ชื่อโมเดลใน LM Studio>
# llama.cpp (llama-server)
ao worker --agent ollama-worker --url http://localhost:8080/v1 --model default
# vLLM
ao worker --agent ollama-worker --url http://localhost:8000/v1 --model <model>
```

เปิดทิ้งไว้ได้เลย — จากนั้น agent ตัวไหนก็ตามสั่งงานโมเดลเล็กได้ด้วย
`ao_handoff(target_agent: "ollama-worker", task: "สรุปไฟล์นี้", artifacts: [...])` แล้วผลลัพธ์จะ
เด้งกลับมาหาเองพร้อมไฟล์แนบ

## ใช้งานประจำวัน

Agent ที่ต่อ MCP เรียก tool เองตามกติกาใน CLAUDE.md/AGENTS.md ส่วนคนใช้ CLI ชุดเดียวกัน:

```bash
ao status                        # ใครถือไม้อยู่ ทำอะไรค้าง (+ notes จากผู้ส่ง)
ao agents                        # ทีม + ความถนัด + ใคร online
ao handoff --from claude-code --to antigravity-ide \
  --task "เจน diagram อธิบาย pipeline" \
  --note "โฟกัส LSTM->DNN flow, ใช้สี minimal" \
  --artifact src/train.py
ao log --agent ollama-worker     # ประวัติ กรองตาม stage/agent ได้
ao watch --from claude-code --to antigravity-ide --notify   # auto-handoff เมื่อไฟล์ .md เปลี่ยน + เด้งแจ้งเตือน
ao projects                      # โปรเจกต์ทั้งหมดที่ลงทะเบียนบนเครื่องนี้
ao stats                         # ตัวเลขกิจกรรม: ใครส่ง/รับเท่าไหร่ ถือไม้นานแค่ไหน
```

## เครื่องมือเสริม (แนวคิดจาก Headroom)

```bash
ao doctor                        # health check ทั้งระบบ — ✔/✖ พร้อมวิธีแก้ทุกข้อ (read-only)
ao doctor --worker-url http://localhost:11434/v1   # + เช็ค model server ด้วย (timeout 2s)

ao setup claude-desktop          # เขียน config Claude Desktop ให้เลย (backup .bak เสมอ,
                                 #  merge ไม่แตะ server อื่น, ปฏิเสธถ้าไฟล์เดิม parse ไม่ได้)
ao setup claude-desktop --remove # ถอดออก — เอาเฉพาะ entry ของเรา
```

**Shared memory ข้ามทีม** — ฝากความรู้/การตัดสินใจให้เพื่อนร่วมทีมโดย*ไม่ต้องส่งไม้*:

```bash
ao memory set db-choice "ใช้ SQLite เพราะ home lab" --agent claude-code --tag decision
ao memory list --tag decision
ao memory get db-choice          # แสดง author + updated_at เสมอ — ผู้อ่านตัดสินความสดเอง
ao memory rm db-choice
```

Agent ใช้ผ่าน MCP tools `ao_remember` / `ao_recall` — เก็บเป็นไฟล์ `.ao/memory/` commit เข้า git ได้
(มี history ตอนถูกเขียนทับ) จำกัด 16KB/รายการ และ**ห้ามฝาก secret** (ทุก agent ในโปรเจกต์อ่านได้หมด)

> 💡 AgentOrchestra ใช้คู่กับ [Headroom](https://github.com/headroomlabs-ai/headroom) ได้เลย —
> Headroom ลดค่า token ระหว่าง agent↔LLM ส่วน AO ประสานงานระหว่าง agent↔agent คนละชั้นกัน
> (`headroom wrap claude` ทำงานร่วมกับ `.mcp.json` ของเราได้ปกติ)

- ทำงานจากที่ไหนก็ได้: `--project my-project` หรือไม่ระบุเลย (ใช้ default)
- ทุก handoff เจน **`HANDOFF.md`** ที่ root — agent ที่ไม่มี MCP (หรือคน) เปิดอ่านสถานะล่าสุดได้ทันที
- Debug: `tail -f .ao/logs/ao.log | jq` (JSON lines, rotate เองที่ 5MB)

## โครงสร้างโปรเจกต์

```
cmd/ao/main.go               # entry point: mcp / worker / CLI dispatch
internal/
  model/        envelope.go      # Standard Payload (+notes) + validation
  state/        stage.go         # state machine (stage ต่อโปรเจกต์)
  workspace/    workspace.go     # .ao/ (source of truth) + Agent capabilities + .agentconfig
  store/        store.go, sqlite.go  # SQLite index (rebuildable, filter stage/agent)
  orchestrator/ orchestrator.go  # business logic กลาง + HANDOFF.md mirror
  registry/     registry.go      # ~/.config/ao/projects.json — เรียกโปรเจกต์ด้วยชื่อ ไม่ต้องพิมพ์ path
  presence/     presence.go      # .ao/presence/ — ใคร online
  memory/       memory.go        # shared memory ข้ามทีม (.ao/memory/, ao_remember/ao_recall)
  doctor/       doctor.go        # ao doctor — read-only health checks
  scaffold/     scaffold.go      # ao init เจน .mcp.json/CLAUDE.md/AGENTS.md + team preset + claude-desktop setup
  worker/       worker.go        # ตัวขับ LLM local (OpenAI-compatible)
  notify/       notify.go        # desktop notification (best-effort)
  logging/      logging.go       # debug log .ao/logs/ao.log
  watcher/      watcher.go       # Automated Artifact Sync (fsnotify)
  mcpserver/    server.go        # MCP tools: ao_init/ao_agents/ao_status/ao_handoff/ao_log/ao_projects
  cli/          cli.go           # CLI subcommands
docs/                            # SPEC, DATA_PIPELINE, DB_SCHEMA, ROADMAP, examples
```

## Dev

```bash
make test    # go test ./...
make vet     # go vet ./...
make build   # go build -o bin/ao ./cmd/ao
```
