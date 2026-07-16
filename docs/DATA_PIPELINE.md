# Standard Payload — Data Pipeline Specification

ทุก handoff ระหว่าง agent ผ่าน AgentOrchestra ใช้โครง JSON เดียวกันนี้เสมอ ไม่ว่าจะมาจาก MCP tool
call (`ao_handoff`) หรือ CLI (`ao handoff`) — ทั้งสองทางประกอบ struct เดียวกันจาก
`internal/model/envelope.go`

## โครงสร้าง

```json
{
  "project_id": "ai-trading-hub",
  "current_stage": "documenting",
  "source_agent": "claude-code",
  "target_agent": "antigravity-ide",
  "payload": {
    "task": "สร้าง infographic อธิบายสถาปัตยกรรม LSTM+DNN meta-labeling",
    "artifacts": [
      "src/models/lstm_primary.py",
      "src/models/dnn_meta.py"
    ],
    "extra": {}
  },
  "metadata": {
    "timestamp": "2026-07-16T12:55:00Z",
    "version": "1.0.0"
  }
}
```

ตัวอย่างไฟล์เต็ม: [`examples/handoff.json`](./examples/handoff.json)

## ฟิลด์

| Field | Type | บังคับ | คำอธิบาย |
|---|---|---|---|
| `project_id` | string | ✅ | ต้องตรงกับ `project_id` ใน `.agentconfig` ของโปรเจกต์ |
| `current_stage` | string | ✅ | ต้องอยู่ในชุด `stages` ที่ `.agentconfig` กำหนด (default: `planning`, `coding`, `documenting`) |
| `source_agent` | string | ✅ | agent ที่ส่งไม้ (ต้องต่างจาก `target_agent`) |
| `target_agent` | string | ✅ | agent ที่รับไม้ต่อ |
| `payload.task` | string | ✅ | สิ่งที่ให้ agent ปลายทางทำต่อ — ข้อความสั้นๆ ที่เป็น instruction ตรงตัว |
| `payload.artifacts` | []string | – | path ไฟล์ที่เกี่ยวข้อง (relative กับ project root) |
| `payload.extra` | object | – | ข้อมูลเสริมแบบ key-value อิสระ (เช่น `architecture_type`, `output_format` ตามสเปคตั้งต้น) |
| `metadata.timestamp` | RFC3339 datetime | ✅ (auto) | เติมอัตโนมัติโดย `ao` ตอนบันทึก |
| `metadata.version` | string | ✅ (auto) | เวอร์ชันของ payload schema ปัจจุบัน (`"1.0.0"`) |

## กติกา Validation (`Envelope.Validate`)

1. `project_id`, `current_stage`, `source_agent`, `target_agent`, `payload.task`, `metadata.version`
   ต้องไม่ว่าง
2. `source_agent` ≠ `target_agent`
3. `current_stage` ต้องอยู่ในชุด stage ที่โปรเจกต์กำหนด (ถ้ามีการกำหนดไว้)

Handoff ที่ validate ไม่ผ่านจะ**ไม่ถูกเขียนลงไฟล์และไม่ถูก index** — ระบบปฏิเสธทันทีพร้อมข้อความ
error บอกว่าฟิลด์ไหนผิด

## เส้นทางของข้อมูล

```
ao_handoff / ao handoff
        │
        ▼
model.Envelope{}.Validate(stages)
        │  ผ่าน
        ▼
workspace.WriteHandoff()
   ├─ เขียน .ao/handoffs/NNN-<source>-to-<target>.json  (atomic write)
   └─ อัปเดต .ao/state.json (stage, holder_agent, last_task ปัจจุบัน)
        │
        ▼
store.SaveHandoff()  →  .ao/index.db (SQLite, สำหรับ query เร็ว)
```

`.ao/handoffs/*.json` คือ source of truth เสมอ — ถ้า `.ao/index.db` หายหรือข้อมูลไม่ตรง ให้รัน
`ao log --reindex` เพื่อ rebuild index จากไฟล์ใหม่ทั้งหมด
