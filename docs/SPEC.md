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

## 4. MCP Tools

ทุก tool อ้างโปรเจกต์ได้ 3 แบบ: `project_dir` (absolute path — ชนะเสมอ), `project_id`
(ชื่อที่ลงทะเบียนใน registry), หรือ**ไม่ระบุเลย** (ใช้ default project) — ดูหัวข้อ 8 Registry

| Tool | หน้าที่ |
|---|---|
| `ao_init(project_dir, project_id, agents[], stages[])` | สร้าง `.ao/` + `.agentconfig` + scaffold (`.mcp.json`, CLAUDE.md, AGENTS.md) + ลงทะเบียน registry (idempotent); ไม่ระบุ agents = ได้ team preset |
| `ao_agents(project…, agent)` | ทีมของโปรเจกต์: ชื่อ, description, capabilities, สถานะ online/last_seen — **tool แรกที่ agent ควรเรียกตอน connect**; ส่ง `agent` (ชื่อตัวเอง) เพื่อให้เพื่อนเห็นว่า online |
| `ao_status(project…, agent)` | stage ปัจจุบัน, ผู้ถือไม้, task + notes ล่าสุด |
| `ao_handoff(project…, source_agent, target_agent, stage, task, notes, artifacts[], extra{})` | validate (รวม target ต้องอยู่ในทีม) แล้วบันทึกการส่งไม้ต่อ + เจน HANDOFF.md |
| `ao_log(project…, limit, stage, agent)` | ประวัติการส่งไม้ต่อ ล่าสุดก่อน กรองได้ |
| `ao_projects()` | โปรเจกต์ทั้งหมดที่ลงทะเบียนบนเครื่อง + ตัวไหนเป็น default |

`ao_init` เจนกติกาลง `CLAUDE.md`/`AGENTS.md` ให้เอง — agent จะถูกสอนว่า *"เริ่ม session เรียก
`ao_agents` → `ao_status`, จบงานเรียก `ao_handoff`"* โดยผู้ใช้ไม่ต้อง copy อะไร

`ao watch` (Automated Artifact Sync, ดูข้อ 6) เป็น **CLI-only** ไม่ใช่ MCP tool เพราะเป็น
long-running process ที่ไม่เข้ากับรูปแบบ request/response ของ MCP tool call — รอ MCP Resources/
subscription (Phase 2 ที่เหลือ) ก่อนถึงจะออกแบบให้ agent สั่ง watch ผ่าน MCP ได้ตรงๆ

## 5. State Machine

Stage ของโปรเจกต์ไม่ได้ hardcode ไว้ 3 ขั้นแบบตายตัว — กำหนดได้ต่อโปรเจกต์ผ่าน `.agentconfig`
(`stages: [planning, coding, documenting]` เป็นค่า default ถ้าไม่ระบุ) `ao_handoff` / `ao handoff`
validate ว่า stage ที่ระบุอยู่ในชุดที่โปรเจกต์กำหนดไว้เท่านั้น ส่วนการเปลี่ยนทิศทาง (forward/backward
เช่นส่งงานกลับจาก documenting ไป coding ใหม่) อนุญาตทั้งหมด ตราบใดที่ทั้งสอง stage อยู่ในชุดที่กำหนด

ดู `internal/state/stage.go`

## 6. Automated Artifact Sync (`ao watch`)

```
ao watch --from claude-code --to antigravity-ide --stage documenting --pattern "*.md" [dir]
```

เฝ้าดู `dir` แบบ recursive (ข้าม `.ao/`, `.git/`, `bin/`, `node_modules/`, `vendor/`, และโฟลเดอร์ที่
ขึ้นต้นด้วย `.` อื่นๆ) ด้วย `fsnotify` เมื่อไฟล์ที่ตรง `--pattern` ใดไฟล์หนึ่งถูกสร้างหรือแก้ไข (มี
debounce 800ms กันยิงซ้ำตอน editor save รัวๆ) จะเรียก `orchestrator.Handoff` ให้เองทันที โดย task
เป็นข้อความบอกว่าไฟล์ไหนเปลี่ยน พร้อมแนบไฟล์นั้นเป็น artifact — ตรงกับฟีเจอร์ "ดักจับไฟล์สเปคโปรเจกต์
แล้วทริกเกอร์ให้ agent สร้างไดอะแกรมใหม่เสมอ" ในสเปคตั้งต้น หยุดด้วย Ctrl+C (SIGINT) หรือ SIGTERM

ดู `internal/watcher/watcher.go`

## 7. Debug Logging

ทุก operation ของ `internal/orchestrator` (`init`, `status`, `handoff`, `log`, `reindex`) เขียน log
เป็น JSON lines ลง `.ao/logs/ao.log` ของโปรเจกต์นั้น (rotate อัตโนมัติเมื่อไฟล์เกิน 5MB) — เป็น
best-effort เสมอ: ถ้าเขียน log ไม่ได้ (เช่น permission ผิด) จะไม่ทำให้ operation หลักล้มเหลว
(`internal/logging.Open` fallback เป็น no-op logger)

MCP server เพิ่มเติมอีกชั้น: ทุก tool call (`ao_init`/`ao_status`/`ao_handoff`/`ao_log`) เขียน trace
(ชื่อ tool, ระยะเวลา, สำเร็จ/ล้มเหลว) ไปที่ stderr ผ่าน `log/slog` — MCP client ส่วนใหญ่ (Claude Code
ฯลฯ) เก็บ stderr ของ MCP server ไว้ให้เองอยู่แล้ว จึงดู log ระดับ "agent เรียก tool อะไรตอนไหน" ได้จาก
ที่นั่นโดยไม่ต้องเปิดไฟล์เพิ่ม

ดู `internal/logging/logging.go`

## 8. Project Registry (เลิกพิมพ์ absolute path)

`~/.config/ao/projects.json` (override ด้วย env `AO_CONFIG_DIR`) เก็บ map `project_id → directory`
— `ao init` ลงทะเบียนให้อัตโนมัติ โปรเจกต์แรกเป็น default ลำดับการ resolve:
`project_dir` (ชนะเสมอ) → `project_id` → default → error พร้อมรายชื่อที่รู้จัก

จำเป็นสำหรับ **Claude Desktop** ซึ่ง config เป็นระดับเครื่อง ไม่รู้จัก "โปรเจกต์ปัจจุบัน" — พอมี registry,
agent เรียก `ao_status()` เปล่าๆ ก็ได้คำตอบของโปรเจกต์ default ทันที ดู `internal/registry/registry.go`

## 9. Capabilities + Presence (ทีมรู้จักกัน)

`.agentconfig` เก็บ agent เป็น object `{name, description, capabilities[]}` (backward compatible กับ
list string เดิมผ่าน custom YAML unmarshal — ดู `internal/workspace`) `ao_agents` คืน roster พร้อม
สถานะ online จาก heartbeat ใน `.ao/presence/<agent>.json` (touch เมื่อ handoff / ระบุตัวเองใน
status/agents / worker poll; online = last_seen < 10 นาที ดู `internal/presence`)

`ao_handoff` validate ว่า `target_agent` อยู่ใน roster — ส่งงานผิดตัว/พิมพ์ชื่อผิดโดน reject ทันที

## 10. `ao worker` — ตัวขับ LLM local

Ollama / LM Studio / llama.cpp / vLLM เป็นแค่ model server ต่อ MCP เองไม่ได้ — `ao worker`
(`internal/worker`) เป็นตัวกลาง: poll `state.json` ทุก `--poll` (default 5s) → ถ้าไม้อยู่ที่ `--agent`:
ประกอบ prompt จาก task + notes + เนื้อไฟล์ artifacts (จำกัด ~32KB) → POST
`{--url}/chat/completions` (OpenAI-compatible จึงรองรับทั้ง 4 ค่ายด้วยโค้ดเดียว) → เขียนผลลง
`.ao/outputs/NNN-<agent>.md` → ส่งไม้กลับหาผู้ส่ง (default `--handoff-back=true`) กันงานซ้ำด้วย
fingerprint ของ handoff ล่าสุด; model server ล่ม = log แล้ว retry รอบถัดไป ไม่ crash

## 11. HANDOFF.md Mirror

ทุก handoff เจน `HANDOFF.md` ที่ root โปรเจกต์ (one-way mirror — เขียนทับเสมอ ไม่อ่านกลับ):
สถานะปัจจุบัน + notes + artifacts + ประวัติ 5 รายการล่าสุด เพื่อให้ agent ที่ไม่มี MCP และมนุษย์
อ่านสถานะได้จากไฟล์เดียว watcher จะไม่ trigger จากไฟล์นี้ (กัน loop)

## 12. ดูเพิ่มเติม

- [`DATA_PIPELINE.md`](./DATA_PIPELINE.md) — Standard Payload / JSON Schema เต็ม
- [`DB_SCHEMA.md`](./DB_SCHEMA.md) — โครงสร้าง SQLite index
- [`ROADMAP.md`](./ROADMAP.md) — Phase ถัดไป
- [`examples/mcp-config.md`](./examples/mcp-config.md) — วิธีต่อ `ao` เข้ากับแต่ละ agent
