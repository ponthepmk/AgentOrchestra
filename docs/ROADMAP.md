# Roadmap

## Phase 1 — MCP Server + CLI (สโคปปัจจุบัน) ✅

- [x] `ao` binary: `ao mcp` (MCP stdio server) + CLI (`init/status/handoff/log`)
- [x] Standard Payload (`model.Envelope`) + validation
- [x] File-based workspace (`.ao/handoffs/*.json`, `.ao/state.json`, `.agentconfig`)
- [x] SQLite index ที่ rebuild ได้ (`ao log --reindex`)
- [x] MCP tools: `ao_init`, `ao_status`, `ao_handoff`, `ao_log`
- [x] เอกสาร: SPEC, DATA_PIPELINE, DB_SCHEMA, ตัวอย่าง MCP config

## Phase 2 — Automated Artifact Sync

- [ ] File watcher (`fsnotify`) เฝ้าดู `.md` / ไฟล์โค้ดที่เปลี่ยน แล้ว auto-trigger `ao_handoff`
      ไปยัง agent ที่ทำ diagram/infographic (ตามสเปคตั้งต้น: "ดักจับไฟล์สเปคโปรเจกต์ ... ทริกเกอร์
      ให้สร้างไดอะแกรมใหม่เสมอ")
- [ ] MCP Resources: expose `.ao/state.json` และ handoff ล่าสุดเป็น MCP resource ที่ agent
      subscribe รับการเปลี่ยนแปลงได้แบบ real-time แทนการ poll ด้วย `ao_status`
- [ ] `ao_log` รองรับ filter ตาม stage / agent

## Phase 3 — Home Lab / K3s

- [ ] `PostgresStore` (implement `store.Store` เดิม) สำหรับ deploy บน K3s แชร์ระหว่างหลายเครื่อง
- [ ] HTTP/SSE transport เพิ่มเติมจาก stdio (`ao serve`) สำหรับ agent ที่รันเป็น remote service
- [ ] Auth แบบง่าย (API token) สำหรับโหมด HTTP
- [ ] Helm chart / K3s manifest สำหรับ deploy `ao serve` + PostgreSQL
- [ ] Integration เฉพาะทางกับ Antigravity IDE (ถ้ามี API ตรงนอกเหนือจาก MCP)

## Out of scope ถาวร (ไม่ทำ เว้นแต่มีคนขอชัดเจน)

- Multi-tenant / multi-user auth ที่ซับซ้อน
- UI แบบ web dashboard (สโคปนี้เป็น CLI/MCP-first ตามที่ตั้งใจไว้)
