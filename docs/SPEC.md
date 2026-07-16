# AgentOrchestra — Functional Specification

## 1. ปัญหาที่แก้

เวลาทำงานกับ AI agent หลายตัว (Claude Code เขียนโค้ด, Antigravity IDE ทำ diagram/infographic,
Codex ช่วยรีวิว ฯลฯ) ในโปรเจกต์เดียวกัน ผู้ใช้ต้อง copy-paste context ระหว่างตัวเอง — บอกแต่ละตัวว่า
"ตอนนี้โปรเจกต์ไปถึงไหนแล้ว", "อีกตัวทำอะไรไปแล้วบ้าง", "ต้องทำอะไรต่อ" ทุกครั้งด้วยมือ

**AgentOrchestra (`ao`)** แก้ปัญหานี้ด้วยการเป็น "วาทยากร" กลาง: agent แต่ละตัวรายงานสถานะและ
ส่งไม้ต่อผ่าน `ao` โดยตรง (ไม่ผ่านมือผู้ใช้) แล้วอ่านสถานะปัจจุบันได้ทุกเมื่อ

## 2. การตัดสินใจด้านสถาปัตยกรรม (และเหตุผล)

| ตัดสินใจ | เลือก | เหตุผล |
|---|---|---|
| รูปแบบการเชื่อมต่อ | **MCP Server** ไม่ใช่ Claude Code plugin | Plugin ผูกกับ Claude Code ค่ายเดียว ส่วน MCP (Model Context Protocol) เป็นมาตรฐานกลางที่ Claude Code, Antigravity, Codex, Cursor ฯลฯ ต่อได้หมด — agent เห็น tool ในเมนูตัวเองแล้วเรียกใช้อัตโนมัติ โดยผู้ใช้ไม่ต้อง copy-paste |
| ภาษา | **Go** | binary เดียว ไม่มี runtime ให้ติดตั้ง เหมาะกับ Home Lab / K3s, มี official MCP Go SDK (`modelcontextprotocol/go-sdk`) รองรับเต็มรูปแบบ |
| Source of truth | **ไฟล์ในโฟลเดอร์ `.ao/`** | commit เข้า git ได้ ทำให้ context ติดไปกับ repo เอง ไม่ต้องพึ่ง service ภายนอกที่อาจหายหรือไม่ sync; ทุก agent ที่มี filesystem access อ่าน/เขียนได้แม้ไม่มี MCP client |
| Storage รอง | **SQLite** (pure-Go, ไม่ใช้ CGO) เป็น *index* ที่ rebuild ได้จากไฟล์เสมอ | เริ่มงานง่าย ไม่ต้องมี service ภายนอก แต่ยัง query เร็วสำหรับ `ao log` / stats; ออกแบบ `Store` interface ไว้ให้สลับเป็น PostgreSQL ได้ใน Phase หลังโดยไม่แตะ business logic |
| รูปแบบใช้งาน | **binary เดียว 2 โหมด**: `ao mcp` (stdio server ให้ agent ต่อ) และ CLI ปกติ (`ao init/status/handoff/log`) ให้คนใช้เอง | logic ชุดเดียว (`internal/orchestrator`) ใช้ร่วมกันทั้งสองโหมด รับประกันว่า agent กับคนเห็นสถานะตรงกันเสมอ |

## 3. สถาปัตยกรรมระบบ

```
[Claude Code]──┐ (MCP / stdio)
[Antigravity]──┼──> ao (Go binary, MCP server) ──> .ao/ files (source of truth)
[Codex]────────┘         │                          └─> SQLite index (.ao/index.db, rebuildable)
                         └─ CLI สำหรับมนุษย์: ao init / status / handoff / log
```

ทุก entry point (MCP tool handler และ CLI subcommand) เรียกเข้า `internal/orchestrator` ชุดเดียวกัน
ซึ่งประสาน 2 ชั้นเก็บข้อมูล:

1. **`internal/workspace`** — จัดการไฟล์ใน `.ao/` (state.json, handoffs/*.json) และ `.agentconfig`
   ที่ root โปรเจกต์ นี่คือ *source of truth* เสมอ
2. **`internal/store`** — SQLite index สำหรับ query เร็ว (`ao log`, ค้นประวัติ) เป็น derived data
   ล้วนๆ ลบทิ้งแล้ว `ao log --reindex` สร้างใหม่จากไฟล์ได้เสมอ

## 4. MCP Tools (Phase 1)

| Tool | หน้าที่ |
|---|---|
| `ao_init(project_dir, project_id, agents[], stages[])` | สร้าง `.ao/` + `.agentconfig` ให้โปรเจกต์ (idempotent) |
| `ao_status(project_dir)` | คืน stage ปัจจุบัน, agent ที่ถือไม้อยู่, task ล่าสุด |
| `ao_handoff(project_dir, source_agent, target_agent, stage, task, artifacts[], extra{})` | validate แล้วบันทึกการส่งไม้ต่อ |
| `ao_log(project_dir, limit)` | ประวัติการส่งไม้ต่อ ล่าสุดก่อน |

Agent ทุกตัวที่ต่อ MCP server นี้จะเห็น tool ทั้ง 4 นี้ในรายการ tool ของตัวเองทันที และเรียกเองได้ตาม
system prompt/instruction ที่ผู้ใช้ตั้งไว้ (เช่น "ก่อนเริ่มงานให้เรียก `ao_status` ก่อนเสมอ")

## 5. State Machine

Stage ของโปรเจกต์ไม่ได้ hardcode ไว้ 3 ขั้นแบบตายตัว — กำหนดได้ต่อโปรเจกต์ผ่าน `.agentconfig`
(`stages: [planning, coding, documenting]` เป็นค่า default ถ้าไม่ระบุ) `ao_handoff` / `ao handoff`
validate ว่า stage ที่ระบุอยู่ในชุดที่โปรเจกต์กำหนดไว้เท่านั้น ส่วนการเปลี่ยนทิศทาง (forward/backward
เช่นส่งงานกลับจาก documenting ไป coding ใหม่) อนุญาตทั้งหมด ตราบใดที่ทั้งสอง stage อยู่ในชุดที่กำหนด

ดู `internal/state/stage.go`

## 6. ดูเพิ่มเติม

- [`DATA_PIPELINE.md`](./DATA_PIPELINE.md) — Standard Payload / JSON Schema เต็ม
- [`DB_SCHEMA.md`](./DB_SCHEMA.md) — โครงสร้าง SQLite index
- [`ROADMAP.md`](./ROADMAP.md) — Phase ถัดไป
- [`examples/mcp-config.md`](./examples/mcp-config.md) — วิธีต่อ `ao` เข้ากับแต่ละ agent
