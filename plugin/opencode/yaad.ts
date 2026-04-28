/**
 * Yaad OpenCode Plugin
 *
 * Gives OpenCode persistent, graph-native memory via Yaad.
 *
 * Install: copy this file to ~/.config/opencode/plugins/yaad.ts
 * Requires: yaad binary in PATH (https://github.com/yaadmemory/yaad)
 */

import type { Plugin } from "@opencode-ai/sdk";

const YAAD_PORT = 3456;
const BASE_URL = `http://127.0.0.1:${YAAD_PORT}`;

// ── helpers ──────────────────────────────────────────────────────────────────

async function isRunning(): Promise<boolean> {
  try {
    const res = await fetch(`${BASE_URL}/yaad/health`, { signal: AbortSignal.timeout(500) });
    return res.ok;
  } catch {
    return false;
  }
}

async function ensureServer(): Promise<void> {
  if (await isRunning()) return;
  // Spawn yaad serve in background
  const proc = Bun.spawn(["yaad", "serve", "--addr", `:${YAAD_PORT}`], {
    stdout: "ignore",
    stderr: "ignore",
    detached: true,
  });
  proc.unref();
  // Wait up to 3s for server to start
  for (let i = 0; i < 30; i++) {
    await Bun.sleep(100);
    if (await isRunning()) return;
  }
}

let sessionID: string | null = null;

async function ensureSession(project: string): Promise<string> {
  if (sessionID) return sessionID;
  try {
    const res = await fetch(`${BASE_URL}/yaad/session/start`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ project, agent: "opencode" }),
    });
    const data = await res.json();
    sessionID = data.session?.id ?? null;
    return sessionID ?? "";
  } catch {
    return "";
  }
}

async function getContext(project: string): Promise<string> {
  try {
    const res = await fetch(`${BASE_URL}/yaad/context?project=${encodeURIComponent(project)}`);
    const data = await res.json();
    if (!data.nodes?.length) return "";

    const lines: string[] = ["## Project Memory (Yaad)\n"];
    const byType: Record<string, string[]> = {};
    for (const n of data.nodes) {
      (byType[n.type] ??= []).push(n.content);
    }
    const sections: [string, string][] = [
      ["convention", "### Conventions"],
      ["task", "### Active Tasks"],
      ["decision", "### Recent Decisions"],
      ["bug", "### Known Bug Patterns"],
    ];
    for (const [type, header] of sections) {
      const items = byType[type];
      if (!items?.length) continue;
      lines.push(header);
      for (const item of items) lines.push(`- ${item}`);
      lines.push("");
    }
    return lines.join("\n");
  } catch {
    return "";
  }
}

async function remember(content: string, type: string, project: string): Promise<void> {
  try {
    await fetch(`${BASE_URL}/yaad/remember`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, type, scope: "project", project, agent: "opencode" }),
    });
  } catch { /* ignore */ }
}

async function endSession(summary: string): Promise<void> {
  if (!sessionID) return;
  try {
    await fetch(`${BASE_URL}/yaad/session/end`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: sessionID, summary }),
    });
    sessionID = null;
  } catch { /* ignore */ }
}

// ── Plugin definition ─────────────────────────────────────────────────────────

export default {
  name: "yaad",
  version: "0.1.0",
  description: "Persistent graph-native memory for OpenCode via Yaad",

  async onLoad() {
    await ensureServer();
  },

  chat: {
    system: {
      async transform(system: string, opts: { cwd: string }): Promise<string> {
        await ensureServer();
        await ensureSession(opts.cwd);
        const ctx = await getContext(opts.cwd);
        if (!ctx) return system;
        return `${system}\n\n${ctx}`;
      },
    },
  },

  session: {
    async onStart(opts: { cwd: string }) {
      await ensureServer();
      await ensureSession(opts.cwd);
    },

    async onEnd(opts: { cwd: string; summary?: string }) {
      if (opts.summary) {
        await remember(opts.summary, "session", opts.cwd);
      }
      await endSession(opts.summary ?? "");
    },
  },

  // Expose yaad MCP tools to OpenCode
  mcp: {
    servers: {
      yaad: {
        command: "yaad",
        args: ["mcp"],
      },
    },
  },
} satisfies Plugin;
