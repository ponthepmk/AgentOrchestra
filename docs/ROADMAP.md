# Roadmap

## Phase 1 — MCP Server + CLI (สโคปปัจจุบัน) ✅

- [x] `ao` binary: `ao mcp` (MCP stdio server) + CLI (`init/status/handoff/log`)
- [x] Standard Payload (`model.Envelope`) + validation
- [x] File-based workspace (`.ao/handoffs/*.json`, `.ao/state.json`, `.agentconfig`)
- [x] SQLite index ที่ rebuild ได้ (`ao log --reindex`)
- [x] MCP tools: `ao_init`, `ao_status`, `ao_handoff`, `ao_log`
- [x] เอกสาร: SPEC, DATA_PIPELINE, DB_SCHEMA, ตัวอย่าง MCP config

## Debug logging (เสริม Phase 1, ทำไปพร้อม Phase 2) ✅

- [x] `internal/logging` — ทุก operation ของ orchestrator (`init`/`status`/`handoff`/`log`/`reindex`)
      และทุก MCP tool call เขียน log แบบ JSON lines ลง `.ao/logs/ao.log` ต่อโปรเจกต์ (rotate อัตโนมัติ
      ที่ 5MB) ใช้ debug ตอนหาว่า handoff ไหนพังเพราะอะไร โดยไม่กระทบการทำงานปกติแม้เขียน log ไม่ได้
      (fallback เป็น no-op logger)
- [x] MCP server เขียน trace แต่ละ tool call (ชื่อ tool, ระยะเวลา, สำเร็จ/ล้มเหลว) ไปที่ stderr
      ผ่าน `log/slog` — client ส่วนใหญ่ (Claude Code ฯลฯ) เก็บ stderr ของ MCP server ไว้ให้เองอยู่แล้ว

## Phase 2 — Automated Artifact Sync

- [x] File watcher (`fsnotify`) — คำสั่ง `ao watch --from <agent> --to <agent> [--pattern glob]... [--stage stage] [dir]`
      เฝ้าดูไฟล์ที่ตรง pattern (default `*.md`) แบบ recursive (ข้าม `.ao/.git/bin/node_modules/vendor`
      และโฟลเดอร์ที่ขึ้นต้นด้วย `.` อื่นๆ) มี debounce กันยิงซ้ำตอน editor save รัว ๆ แล้ว auto-trigger
      `ao_handoff` ไปยัง agent ที่ทำ diagram/infographic ทันทีที่ไฟล์เปลี่ยน (ตามสเปคตั้งต้น: "ดักจับไฟล์
      สเปคโปรเจกต์ ... ทริกเกอร์ให้สร้างไดอะแกรมใหม่เสมอ") — ดู `internal/watcher/watcher.go`
- [x] `ao_log` / `ao log` รองรับ filter ตาม `--stage` และ `--agent` แล้ว
- [ ] MCP Resources: expose `.ao/state.json` และ handoff ล่าสุดเป็น MCP resource ที่ agent
      subscribe รับการเปลี่ยนแปลงได้แบบ real-time แทนการ poll ด้วย `ao_status`
- [ ] `ao watch` เป็น MCP tool ด้วย (ตอนนี้เป็น CLI-only เพราะเป็น long-running process ไม่เข้ากับ
      MCP tool แบบ request/response — ต้องรอ MCP Resources/subscription ก่อนถึงจะออกแบบตรงนี้ได้ดี)

## Phase 3 — Home Lab / K3s

- [ ] `PostgresStore` (implement `store.Store` เดิม) สำหรับ deploy บน K3s แชร์ระหว่างหลายเครื่อง
- [ ] HTTP/SSE transport เพิ่มเติมจาก stdio (`ao serve`) สำหรับ agent ที่รันเป็น remote service
- [ ] Auth แบบง่าย (API token) สำหรับโหมด HTTP
- [ ] Helm chart / K3s manifest สำหรับ deploy `ao serve` + PostgreSQL
- [ ] Integration เฉพาะทางกับ Antigravity IDE (ถ้ามี API ตรงนอกเหนือจาก MCP)

## Out of scope ถาวร (ไม่ทำ เว้นแต่มีคนขอชัดเจน)

- Multi-tenant / multi-user auth ที่ซับซ้อน
- UI แบบ web dashboard (สโคปนี้เป็น CLI/MCP-first ตามที่ตั้งใจไว้)
