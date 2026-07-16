# AgentOrchestra (AO)

**Multi-Agent Orchestrator** — ตัว "วาทยากร" ที่ส่งต่อ context ระหว่าง AI agent หลายตัว
(Claude Code, Codex, Antigravity IDE ฯลฯ) ในโปรเจกต์เดียวกัน โดยไม่ต้อง copy-paste เอง

ต่อผ่าน **MCP (Model Context Protocol)** — มาตรฐานกลางที่ agent เกือบทุกค่ายรองรับ — ไม่ใช่ plugin
เฉพาะของ tool ใดตัวหนึ่ง เขียนด้วย **Go** เป็น binary เดียว รันได้ทั้งบนเครื่องตัวเองและ K3s Home Lab

## สถาปัตยกรรม

```
[Claude Code]──┐ (MCP / stdio)
[Antigravity]──┼──> ao (Go binary, MCP server) ──> .ao/ files (source of truth)
[Codex]────────┘         │                          └─> SQLite index (.ao/index.db, rebuildable)
                         └─ CLI สำหรับมนุษย์: ao init / status / handoff / log
```

- **`.ao/` ในโปรเจกต์เป้าหมาย = source of truth** — เป็นไฟล์ธรรมดา commit เข้า git ได้ ติดไปกับ repo
- **SQLite = index รอง** ที่ rebuild ได้เสมอจากไฟล์ (`ao log --reindex`)
- **binary เดียว 2 โหมด:** `ao mcp` (ให้ agent ต่อผ่าน MCP) และ CLI ปกติ (ให้คนสั่งเอง) — logic
  ชุดเดียวกันทั้งคู่ (`internal/orchestrator`) จึงเห็นสถานะตรงกันเสมอ

รายละเอียดเหตุผลการออกแบบทั้งหมด: [`docs/SPEC.md`](./docs/SPEC.md)

## เริ่มใช้งาน

```bash
make build          # ได้ bin/ao
cp bin/ao /usr/local/bin/ao   # หรือใส่ PATH เอง
```

### 1) ต่อเข้ากับ agent ที่ใช้

ดูตัวอย่าง config แต่ละตัวที่ [`docs/examples/mcp-config.md`](./docs/examples/mcp-config.md)
(Claude Code, Codex CLI, Antigravity IDE)

### 2) เริ่มโปรเจกต์

```bash
cd your-project
ao init --id ai-trading-hub --agents claude-code,antigravity-ide
```

สร้าง `.ao/` (state + ประวัติ handoff) และ `.agentconfig` ในโปรเจกต์นั้น

### 3) ใช้งานประจำวัน

Agent ที่ต่อ MCP ไว้แล้วจะเรียก `ao_status` / `ao_handoff` เองตาม instruction ที่ตั้งไว้ ส่วนคนก็สั่ง
CLI เดียวกันได้ตรงๆ:

```bash
ao status                                     # ดูว่าใครถือไม้อยู่ ทำอะไรค้างไว้
ao handoff --from claude-code --to antigravity-ide \
  --stage documenting \
  --task "สร้าง infographic อธิบาย LSTM+DNN architecture" \
  --artifact src/models/lstm_primary.py
ao log                                        # ดูประวัติทั้งหมด
```

## จำลองการทำงานแบบเต็ม (Simulation)

สมมติโปรเจกต์ `ai-trading-hub` (Meta-Labeling EA) กำลังจะเข้าขั้นทำเอกสาร:

```
$ ao status
project:      ai-trading-hub
stage:        coding
holder_agent: claude-code
last_task:    เขียน purged walk-forward CV
```

**Claude Code** ทำงานเสร็จ (เขียนโค้ด `src/validation/purged_cv.py` เสร็จแล้ว) จึงเรียก tool
`ao_handoff` (ผ่าน MCP โดยอัตโนมัติ ไม่ต้องมีคนสั่ง):

```json
{
  "project_dir": "/home/user/Ai-trading-hub",
  "source_agent": "claude-code",
  "target_agent": "antigravity-ide",
  "stage": "documenting",
  "task": "สร้าง diagram อธิบาย purged walk-forward CV และวิธีกัน data leakage",
  "artifacts": ["src/validation/purged_cv.py"]
}
```

เบื้องหลัง `ao`:
1. validate payload (มี `task`, stage `documenting` อยู่ในชุดที่ `.agentconfig` กำหนดไว้ไหม)
2. เขียนไฟล์ `.ao/handoffs/007-claude-code-to-antigravity-ide.json`
3. อัปเดต `.ao/state.json` → stage เป็น `documenting`, holder เป็น `antigravity-ide`
4. index ลง `.ao/index.db` สำหรับ query เร็ว

**Antigravity IDE** (ต่อ MCP server เดียวกัน) เปิดงานมาแล้วเรียก `ao_status` เองก่อนเริ่ม เห็นว่า
ตัวเองถือไม้อยู่ พร้อม `task` และ `artifacts` ที่ Claude Code ทิ้งไว้ให้ครบ — ไม่ต้องให้ผู้ใช้อธิบายซ้ำ
เลย ทำ diagram เสร็จก็ `ao_handoff` ส่งต่อกลับหรือส่งไปขั้นถัดไปเช่นกัน

```
$ ao log --limit 3
[2026-07-16T13:10:00Z] antigravity-ide -> claude-code (coding): แก้ diagram ตามคอมเมนต์
[2026-07-16T13:05:00Z] claude-code -> antigravity-ide (documenting): สร้าง diagram อธิบาย purged walk-forward CV
[2026-07-16T12:40:00Z] claude-code -> claude-code (coding): เขียน purged walk-forward CV
```

ผู้ใช้ไม่ต้องเข้าไปสั่งเองระหว่างขั้นตอนเหล่านี้เลย — เห็นแค่ผลลัพธ์สุดท้ายและเรียก `ao status` /
`ao log` เพื่อตรวจสอบความคืบหน้าได้ตลอดเวลา

## โครงสร้างโปรเจกต์

```
cmd/ao/main.go               # entry point: dispatch "mcp" vs CLI subcommands
internal/
  model/       envelope.go   # Standard Payload + validation
  state/       stage.go      # state machine (stage ต่อโปรเจกต์)
  workspace/   workspace.go  # จัดการ .ao/ (source of truth)
  store/       store.go, sqlite.go  # SQLite index (rebuildable)
  orchestrator/orchestrator.go      # business logic กลาง ใช้ร่วมกันทั้ง MCP และ CLI
  mcpserver/   server.go     # MCP tools: ao_init/ao_status/ao_handoff/ao_log
  cli/         cli.go        # CLI subcommands
docs/
  SPEC.md              # ข้อกำหนดทางเทคนิคเต็ม + เหตุผลการตัดสินใจ
  DATA_PIPELINE.md     # Standard Payload schema เต็ม
  DB_SCHEMA.md         # โครงสร้าง SQLite index
  ROADMAP.md           # แผนขั้นถัดไป
  examples/            # ตัวอย่าง payload + วิธีต่อ MCP กับแต่ละ agent
```

## Dev

```bash
make test    # go test ./...
make vet     # go vet ./...
make build   # go build -o bin/ao ./cmd/ao
```
