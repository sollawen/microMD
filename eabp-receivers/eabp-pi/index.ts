import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import * as net from "node:net";
import * as fs from "node:fs";
import * as path from "node:path";

const PROTOCOL = "eabp-1";
const WIDGET = "mnab";

export default function (pi: ExtensionAPI) {
  let server: net.Server | null = null;
  let name = "", socketPath = "", regFile = "";
  let pending: any = null;            // M2：纯上下文暂存

  function registryDir(): string {
    const override = process.env.MNAB_REG_DIR;
    if (override) return override;
    const base = process.env.XDG_RUNTIME_DIR
      || process.env.TMPDIR
      || "/tmp";
    return path.join(base, `microneo-agent-bridge-${process.getuid?.() ?? 0}`);
  }

  pi.on("session_start", async (_e, ctx) => {
    name = `pi-${process.pid}`;        // PID 保证唯一，规避冲突处理（见 §八 风险3）
    const dir = registryDir();
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    socketPath = path.join(dir, `receiver-${name}.sock`);
    regFile    = path.join(dir, `receiver-${name}.json`);

    server = net.createServer((conn) => {
      let buf = "";
      conn.on("data", (chunk) => {
        buf += chunk.toString("utf8");
        let nl;
        while ((nl = buf.indexOf("\n")) >= 0) {
          handleLine(buf.slice(0, nl), ctx);
          buf = buf.slice(nl + 1);
        }
      });
    });
    await new Promise<void>((res, rej) => {
      server!.listen(socketPath, () => res());
      server!.on("error", rej);
    });
    fs.chmodSync(socketPath, 0o600);
    fs.writeFileSync(regFile, JSON.stringify({
      name, pid: process.pid, transport: "unix",
      socket: socketPath, protocol: PROTOCOL,
      startedAt: Math.floor(Date.now()/1000), cwd: process.cwd(), labels: ["default"],
    }));
  });

  pi.on("session_shutdown", async () => {
    if (server) {
      await new Promise<void>((res) => server!.close(() => res()));
      server = null;
    }
    try { fs.unlinkSync(regFile); } catch {}
    try { fs.unlinkSync(socketPath); } catch {}
    pending = null;
  });

  function handleLine(line: string, ctx: any) {
    let env: any;
    try { env = JSON.parse(line); } catch { return; }
    if (env.v !== 1 || env.type !== "context") return;   // §7.2 主版本校验
    onContext(env, ctx);                                 // 传完整信封（M1 要读 sender）
  }

  function onContext(env: any, ctx: any) {
    const p = env.payload, s = env.sender;
    // —— M1：纯 UI 展示 ——
    const from = s?.instance ?? s?.name ?? "?";
    const sel = p.selection ? ` [${p.selection.start.line}-${p.selection.end.line}]` : "";
    const msg = p.message ? ` → "${p.message}"` : "";
    ctx.ui.notify(`${from}: ${p.path}:${p.cursor?.line}${sel}${msg}`, "info");

    // —— M2 增量（替换上面五行）：见 §七 ——
  }

  // —— M2 增量：before_agent_start 注入纯上下文 ——
  // pi.on("before_agent_start", async (_e, ctx) => {
  //   if (!pending) return;
  //   const block = formatContext(pending);
  //   pending = null;
  //   ctx.ui.setWidget(WIDGET, undefined);
  //   return { message: { customType: "mnab-context", content: block, display: true } };
  // });

  // —— M2 增量：/mnab 命令 ——
  // pi.registerCommand("mnab", { ... });   // paste/send/discard/status
}