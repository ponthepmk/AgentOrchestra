# เริ่มใช้ AgentOrchestra ใน 3 ขั้น

## ① ติดตั้ง (ครั้งเดียวต่อเครื่อง)

**ทางง่าย:** ดาวน์โหลด binary สำหรับ OS ของคุณจากหน้า
[Releases](../../releases) แล้ววางใน PATH:

```bash
# macOS / Linux (เปลี่ยนชื่อไฟล์ตามที่ดาวน์โหลด)
chmod +x ao_v*_* && sudo mv ao_v*_* /usr/local/bin/ao
```

**ทางเลือก (มี Go >= 1.25):** `make build && cp bin/ao /usr/local/bin/ao`

## ② เปิดใช้ในโปรเจกต์ (ครั้งเดียวต่อโปรเจกต์)

```bash
cd โปรเจกต์ของคุณ
ao init --id ชื่อโปรเจกต์
```

จบ — คำสั่งเดียวได้: ทีม agent มาตรฐาน 4 ตัวพร้อมความถนัด (claude-code, codex,
antigravity-ide, ollama-worker), config ให้ Claude Code (`.mcp.json`), กติกาให้ agent เรียกใช้เอง
(`CLAUDE.md`/`AGENTS.md`), และลงทะเบียนโปรเจกต์ไว้เรียกจากที่ไหนก็ได้

ใช้ Claude Desktop ด้วย? เพิ่มอีกคำสั่งเดียว: `ao setup claude-desktop` (แล้ว restart แอป)

## ③ ใช้งาน

```bash
ao      # หน้าสรุป: ถึงตาใคร งานอะไรค้าง ใคร online ควรทำอะไรต่อ
ao ui   # dashboard ในเบราว์เซอร์ — เห็นทุกอย่างหน้าเดียว refresh สดเอง
```

เปิดแอป AI ของคุณ (Claude Code / Claude Desktop / Antigravity) แล้วทำงานตามปกติ —
agent จะเช็คงานและส่งไม้ต่อกันเองผ่าน tools `ao_*` ตามกติกาที่ `ao init` วางไว้ให้

ใช้โมเดล local (Ollama ฯลฯ) เป็นลูกทีมด้วย? เปิดทิ้งไว้:

```bash
ao worker --agent ollama-worker --url http://localhost:11434/v1 --model qwen2.5:3b
```

---

**มีปัญหา?** → `ao doctor` บอกจุดพังพร้อมวิธีแก้ทุกข้อ
**อยากรู้ลึก?** → [README.md](./README.md) และ [docs/SPEC.md](./docs/SPEC.md)
